package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"kaikku/internal/deezer"
)

// Импорт плейлиста из Spotify или списка треков. Работает так:
//
//  1. Список треков (название, исполнители, длительность). Ссылку Spotify
//     разбирает сервер (handleImportSpotify): страница встраиваемого плеера
//     open.spotify.com/embed/... отдаётся без входа и содержит первые 100
//     треков. Web API Spotify с 2026 года требует Premium у владельца
//     приложения, поэтому его не используем. Текстовый список и CSV разбирает
//     фронт (lib/importList.js).
//  2. Фронт шлёт треки пачками в handleImportMatch, сервер ищет каждый в
//     Deezer и выбирает лучший вариант по названию, исполнителю,
//     длительности и версии (explicit или clean). Пачками — чтобы показывать прогресс и не занимать
//     общий лимит Deezer надолго.
//  3. Найденные треки фронт присылает в handleImportAlbums: треки одного
//     исполнителя сводятся на один релиз (import_albums.go).
//  4. Найденное фронт сохраняет обычным плейлистом (без фонового
//     скачивания: сотня треков разом — лишняя нагрузка на YouTube; трек
//     скачается, когда его включат).

const (
	maxImportMatch    = 20 // треков в одном запросе /import/match
	importParallel    = 3  // одновременных поисков в Deezer на запрос
	maxSpotifyPage    = 5 << 20
	spotifyEmbedLimit = 100 // больше встраиваемый плеер не показывает
)

// Поиски импорта — не больше половины доли API в лимите Deezer (40 за 5 с,
// deezer.go): импорт сотни треков не должен тормозить страницы остальным.
var importLimit = deezer.NewLimiter(20, 5*time.Second)

type importTrack struct {
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	DurationMs int      `json:"duration_ms,omitempty"`
	// Explicit — версия с ненормативной лексикой; nil — источник не знает
	// (текстовый список). У Deezer clean- и explicit-версии — разные треки с
	// одинаковыми названием и длительностью.
	Explicit *bool `json:"explicit,omitempty"`
}

type importList struct {
	Title     string        `json:"title"`
	Tracks    []importTrack `json:"tracks"`
	Truncated bool          `json:"truncated"` // в источнике, возможно, есть ещё треки
}

// --- Spotify ---

var spotifyLinkRe = regexp.MustCompile(`(?:open\.spotify\.com/(?:intl-[a-zA-Z-]+/)?(?:embed/)?|spotify:)(playlist|album)[/:]([A-Za-z0-9]{22})`)

// spotifyEmbedURL is a var so tests can point it at a fake server.
var spotifyEmbedURL = "https://open.spotify.com/embed/"

var spotifyHTTP = &http.Client{Timeout: 15 * time.Second}

func (s *Server) handleImportSpotify(w http.ResponseWriter, r *http.Request) {
	m := spotifyLinkRe.FindStringSubmatch(r.URL.Query().Get("link"))
	if m == nil {
		writeError(w, http.StatusBadRequest, "a Spotify playlist or album link is required")
		return
	}
	list, err := fetchSpotifyList(m[1], m[2])
	if err != nil {
		log.Printf("spotify import %s/%s: %v", m[1], m[2], err)
		writeError(w, http.StatusBadGateway, "could not read the Spotify playlist — is it public?")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func fetchSpotifyList(kind, id string) (*importList, error) {
	req, err := http.NewRequest(http.MethodGet, spotifyEmbedURL+kind+"/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en")
	resp, err := spotifyHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spotify answered %s", resp.Status)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxSpotifyPage))
	if err != nil {
		return nil, err
	}
	return parseSpotifyEmbed(page)
}

var nextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)

// parseSpotifyEmbed reads the track list out of the embed page's Next.js data.
func parseSpotifyEmbed(page []byte) (*importList, error) {
	m := nextDataRe.FindSubmatch(page)
	if m == nil {
		return nil, errors.New("no __NEXT_DATA__ on the page")
	}
	var data struct {
		Props struct {
			PageProps struct {
				State struct {
					Data struct {
						Entity struct {
							Name      string `json:"name"`
							Title     string `json:"title"`
							TrackList []struct {
								Title      string `json:"title"`
								Subtitle   string `json:"subtitle"`
								Duration   int    `json:"duration"`
								IsExplicit *bool  `json:"isExplicit"`
							} `json:"trackList"`
						} `json:"entity"`
					} `json:"data"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(m[1], &data); err != nil {
		return nil, fmt.Errorf("embed data: %w", err)
	}
	e := data.Props.PageProps.State.Data.Entity
	list := &importList{Title: html.UnescapeString(firstNonEmpty(e.Name, e.Title)), Tracks: []importTrack{}}
	for _, t := range e.TrackList {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		// Исполнители — одной строкой «A, B»; имена с запятой («Tyler, The
		// Creator») так разобьются, но для поиска важен первый, а сверка
		// идёт по всей строке (artistScore).
		var artists []string
		for _, a := range strings.Split(t.Subtitle, ",") {
			if a = strings.TrimSpace(html.UnescapeString(a)); a != "" {
				artists = append(artists, a)
			}
		}
		list.Tracks = append(list.Tracks, importTrack{Title: html.UnescapeString(t.Title), Artists: artists, DurationMs: t.Duration, Explicit: t.IsExplicit})
	}
	if len(list.Tracks) == 0 {
		return nil, errors.New("no tracks in the embed data")
	}
	list.Truncated = len(e.TrackList) >= spotifyEmbedLimit
	return list, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// --- сопоставление с Deezer ---

func (s *Server) handleImportMatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tracks []importTrack `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Tracks) == 0 || len(req.Tracks) > maxImportMatch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("tracks: 1–%d songs are required", maxImportMatch))
		return
	}
	matches := make([]*deezer.Track, len(req.Tracks))
	errs := make([]error, len(req.Tracks))
	slots := make(chan struct{}, importParallel)
	var wg sync.WaitGroup
	for i, it := range req.Tracks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			errs[i] = guard("import match", func() (err error) { matches[i], err = s.matchImportTrack(it); return })
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			log.Printf("import match: %v", err)
			writeDeezerError(w, "deezer search failed", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

// matchImportTrack finds the song in Deezer. nil — nothing similar enough.
//
// Запросы (проверено на живом Deezer, 2026-10): `track:"название"
// исполнитель` ставит нужный трек первым; `artist:"…"` поиск Deezer сейчас
// не понимает (0 результатов), а просто «исполнитель название» часто
// отдаёт live-версию. Результатов — 25: часть Deezer отфильтровывает
// (недоступно в стране сервера), и из 10 остаётся мало.
func (s *Server) matchImportTrack(it importTrack) (*deezer.Track, error) {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return nil, nil
	}
	clean := strings.ReplaceAll(searchTitle(title), `"`, "")
	queries := []string{fmt.Sprintf(`track:"%s"`, clean)}
	if len(it.Artists) > 0 {
		artist := strings.ReplaceAll(it.Artists[0], `"`, "")
		queries = []string{fmt.Sprintf(`track:"%s" %s`, clean, artist), artist + " " + clean}
	}
	// Нашлась не та версия (clean вместо explicit или наоборот) — пробуем
	// следующий запрос: нужная могла не попасть в выдачу первого.
	var best *deezer.Track
	bestRank := 0.0
	for _, q := range queries {
		importLimit.Wait()
		found, err := s.Deezer.Search(q, 25)
		if err != nil {
			return nil, err
		}
		if c, rank := bestImportMatch(it, found); c != nil && rank > bestRank {
			best, bestRank = c, rank
		}
		if best != nil && sameVersion(it, best) {
			break
		}
	}
	return best, nil
}

// sameVersion: explicit у кандидата такой же, как в источнике (или
// источник этого не знает).
func sameVersion(it importTrack, c *deezer.Track) bool {
	return it.Explicit == nil || *it.Explicit == c.Explicit
}

// bestImportMatch scores the candidates; the best one wins if the title and
// the artist really match (and the length, when known, is close). rank —
// его оценка с учётом версии: clean вместо explicit (и наоборот) годится,
// только если нужной версии нет.
func bestImportMatch(it importTrack, found []deezer.Track) (best *deezer.Track, rank float64) {
	for i := range found {
		c := &found[i]
		ts := titleScore(it.Title, c.Title)
		as := 1.0 // список без исполнителя («только название») — верим поиску
		if len(it.Artists) > 0 {
			as = artistScore(it.Artists, c)
		}
		ds := durationScore(it.DurationMs, c.DurationMs)
		if ts < 0.6 || as < 0.7 || ds == 0 {
			continue
		}
		score := 0.5*ts + 0.3*as + 0.2*ds
		if score < 0.7 {
			continue
		}
		if !sameVersion(it, c) {
			score -= 0.2
		}
		if score > rank {
			best, rank = c, score
		}
	}
	return best, rank
}

// Диакритика латиницы — без пакета нормализации Unicode (лишняя зависимость
// ради сравнения имён): «Beyoncé» = «Beyonce», «Sigur Rós» = «Sigur Ros».
var foldDiacritics = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ā", "a", "ą", "a",
	"ç", "c", "č", "c", "ć", "c", "ď", "d", "đ", "d",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ē", "e", "ę", "e", "ě", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ī", "i", "ı", "i",
	"ł", "l", "ñ", "n", "ń", "n", "ň", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "ō", "o", "ő", "o",
	"ř", "r", "š", "s", "ś", "s", "ş", "s", "ß", "ss", "ť", "t", "ţ", "t",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ū", "u", "ů", "u", "ű", "u",
	"ý", "y", "ÿ", "y", "ž", "z", "ź", "z", "ż", "z",
	"æ", "ae", "œ", "oe", "ё", "е",
)

// normalizeName: lower case, no diacritics, punctuation as spaces, & = and.
func normalizeName(s string) string {
	s = strings.ReplaceAll(foldDiacritics.Replace(strings.ToLower(s)), "&", " and ")
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	return b.String()
}

// Spotify пишет версии через дефис («Song - Remastered 2011», «Song - Radio
// Edit»), Deezer — в скобках; фиты бывают и там, и там.
var (
	titleNoteRe = regexp.MustCompile(`\s+[-–—]\s+.*$`)
	bracketsRe  = regexp.MustCompile(`\s*[(\[][^)\]]*[)\]]`)
	featRe      = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|with)\s+.*$`)
)

// bareTitle drops brackets (first: Deezer puts dashes inside them, «Get
// Lucky (Radio Edit - feat. …)»), then a dash note and featured artists.
func bareTitle(t string) string {
	t = bracketsRe.ReplaceAllString(t, "")
	t = titleNoteRe.ReplaceAllString(t, "")
	return strings.TrimSpace(featRe.ReplaceAllString(t, ""))
}

// baseTitle is the comparable form of bareTitle.
func baseTitle(t string) string {
	return normalizeName(bareTitle(t))
}

// searchTitle — для запроса в Deezer: без пометок, но со словами названия.
func searchTitle(t string) string {
	if b := bareTitle(t); b != "" {
		return b
	}
	return t
}

func titleScore(want, got string) float64 {
	a, b := baseTitle(want), baseTitle(got)
	if a == "" || b == "" {
		return 0
	}
	score := 0.0
	switch {
	case a == b:
		score = 1
	case strings.Contains(a, b) || strings.Contains(b, a):
		score = 0.8
	case similar(latin(a), latin(b)): // «Kontrakt» и «Контракт»
		score = 0.8
	default:
		score = wordOverlap(a, b)
	}
	// Ремикс, live или акустика — не то же самое, что оригинал.
	if versionWords(want) != versionWords(got) {
		score -= 0.3
	}
	return score
}

var versionRe = regexp.MustCompile(`(?i)\b(remix|live|acoustic|instrumental|karaoke|sped up|slowed|nightcore)\b`)

func versionWords(t string) string {
	words := versionRe.FindAllString(strings.ToLower(t), -1)
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

func wordOverlap(a, b string) float64 {
	wa, wb := strings.Fields(a), strings.Fields(b)
	set := map[string]bool{}
	for _, w := range wa {
		set[w] = true
	}
	common := 0
	for _, w := range wb {
		if set[w] {
			common++
		}
	}
	total := len(wa)
	if len(wb) > total {
		total = len(wb)
	}
	if total == 0 {
		return 0
	}
	return float64(common) / float64(total)
}

// artistScore: 1 — исполнитель кандидата есть в списке (или наоборот),
// 0.7 — частично («Tyler» в «Tyler, The Creator») или то же имя другим
// алфавитом («Tikhaya Burya» в Spotify = «Тихая Буря» в Deezer), 0 — другой.
func artistScore(want []string, c *deezer.Track) float64 {
	all := normalizeName(strings.Join(want, " "))
	names := []string{c.Artist}
	for _, ref := range c.Contributors {
		names = append(names, ref.Name)
	}
	best := 0.0
	for _, name := range names {
		n := normalizeName(name)
		if n == "" {
			continue
		}
		for _, w := range want {
			if normalizeName(w) == n {
				return 1
			}
		}
		if containsWords(all, n) || containsWords(n, normalizeName(want[0])) {
			best = 0.7
			continue
		}
		for _, w := range want {
			if similar(latin(normalizeName(w)), latin(n)) {
				best = 0.7
			}
		}
	}
	return best
}

// Кириллица латиницей — как её обычно пишут в названиях на Spotify.
var cyrillicToLatin = strings.NewReplacer(
	"а", "a", "б", "b", "в", "v", "г", "g", "ґ", "g", "д", "d", "е", "e", "є", "ye",
	"ж", "zh", "з", "z", "и", "i", "і", "i", "ї", "yi", "й", "y", "к", "k", "л", "l",
	"м", "m", "н", "n", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t", "у", "u",
	"ф", "f", "х", "kh", "ц", "ts", "ч", "ch", "ш", "sh", "щ", "shch", "ъ", "", "ы", "y",
	"ь", "", "э", "e", "ю", "yu", "я", "ya",
)

func latin(s string) string { return cyrillicToLatin.Replace(s) }

// similar: почти одно и то же написание (транслитерацию делают по-разному:
// «molli» и «molly») — не больше одной правки на шесть букв.
func similar(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	longest := len(ra)
	if len(rb) > longest {
		longest = len(rb)
	}
	return longest >= 4 && editDistance(ra, rb)*6 <= longest
}

func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// containsWords: s contains sub as whole words.
func containsWords(s, sub string) bool {
	return sub != "" && strings.Contains(" "+s+" ", " "+sub+" ")
}

// durationScore: 0 — точно другая запись (разница больше 30 с).
func durationScore(wantMs, gotMs int) float64 {
	if wantMs <= 0 || gotMs <= 0 {
		return 0.5
	}
	d := wantMs - gotMs
	if d < 0 {
		d = -d
	}
	switch {
	case d <= 3000:
		return 1
	case d <= 10000:
		return 0.7
	case d <= 30000:
		return 0.3
	}
	return 0
}
