package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Описание артиста (миграция 0024) — блок «Об артисте» на его странице.
//
// Откуда: свой текст (у своего артиста — его владелец, у любого — админ)
// главнее всего. Иначе ищем у Deezer (по ID — точно тот артист), потом на
// Last.fm (по имени), и берём текст на языке интерфейса; если на нём нет
// нигде — какой нашёлся. Найденное хранится месяц, «ничего нет» — неделю.

const (
	bioMaxRunes  = 20000
	bioTTL       = 30 * 24 * time.Hour
	bioEmptyTTL  = 7 * 24 * time.Hour
	bioFetchTime = 15 * time.Second
	// Жанров под описанием — не больше стольких.
	artistPageTags = 5
)

type artistBio struct {
	Text   string `json:"text"`
	Source string `json:"source"` // custom | deezer | lastfm | none
	URL    string `json:"url,omitempty"`
}

func bioLang(r *http.Request) string {
	if r.URL.Query().Get("lang") == "ru" {
		return "ru"
	}
	return "en"
}

// bioOwner — владелец своего артиста ("" — нет или артист Deezer).
func (s *Server) bioOwner(id string) string {
	var owner string
	s.DB.QueryRow(`SELECT coalesce(owner_id::text, '') FROM artists WHERE deezer_id = $1`, id).Scan(&owner)
	return owner
}

// GET /api/discover/artists/{id}/bio?lang=ru|en
func (s *Server) handleArtistBio(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	v := viewerOf(r)
	owner := ""
	if isLocalID(id) {
		owner = s.bioOwner(id)
	}
	// Теги (tags.go) — заодно, параллельно с описанием: оба могут ждать Last.fm.
	var tags []artistTag
	var tagsCustom bool
	tagsDone := make(chan struct{})
	go func() {
		defer close(tagsDone)
		guard("artist tags", func() error { tags, tagsCustom = s.artistTags(r.Context(), id); return nil })
	}()
	bio := s.artistBio(r.Context(), id, bioLang(r))
	<-tagsDone
	writeJSON(w, http.StatusOK, map[string]any{
		"text": bio.Text, "source": bio.Source, "url": bio.URL,
		// Править: свой артист — владелец (и админ в режиме админа), Deezer — админ в режиме админа.
		"editable":    v.editable(owner),
		"tags":        tagNames(tags),
		"tags_custom": tagsCustom,
	})
}

func (s *Server) artistBio(ctx context.Context, id, lang string) artistBio {
	var b artistBio
	var updated time.Time
	err := s.DB.QueryRow(
		`SELECT text, source, url, updated_at FROM artist_bios WHERE deezer_id = $1 AND lang IN ('', $2) ORDER BY lang LIMIT 1`, id, lang,
	).Scan(&b.Text, &b.Source, &b.URL, &updated)
	if err == nil {
		ttl := bioTTL
		if b.Source == "none" {
			ttl = bioEmptyTTL
		}
		if b.Source == "custom" || time.Since(updated) < ttl {
			return b
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("bio of %s: %v", id, err)
	}
	if isLocalID(id) {
		return artistBio{Source: "none"} // своего артиста нет ни у Deezer, ни на Last.fm
	}
	fresh, ok := s.fetchBio(ctx, id, lang)
	if !ok && fresh.Text == "" {
		if err == nil {
			return b // источники не ответили — пусть будет старое
		}
		return fresh // и не запоминаем «ничего нет»: это сбой, а не пустота
	}
	if _, err := s.DB.Exec(
		`INSERT INTO artist_bios (deezer_id, lang, text, source, url) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (deezer_id, lang) DO UPDATE SET text = EXCLUDED.text, source = EXCLUDED.source, url = EXCLUDED.url, updated_at = now()`,
		id, lang, fresh.Text, fresh.Source, fresh.URL,
	); err != nil {
		log.Printf("bio of %s: save: %v", id, err)
	}
	return fresh
}

// fetchBio спрашивает Deezer, а если у него нет текста на нужном языке —
// и Last.fm. ok = false — какой-то источник не ответил.
func (s *Server) fetchBio(ctx context.Context, id, lang string) (artistBio, bool) {
	ctx, cancel := context.WithTimeout(ctx, bioFetchTime)
	defer cancel()
	ok := true
	var dz artistBio
	if s.DeezerSite != nil {
		text, err := s.DeezerSite.ArtistBio(ctx, id, lang)
		if err != nil {
			log.Printf("bio of %s: deezer: %v", id, err)
			ok = false
		}
		dz = artistBio{Text: bioText(text), Source: "deezer", URL: "https://www.deezer.com/artist/" + id}
		if dz.Text != "" && inLang(dz.Text, lang) {
			return dz, ok
		}
	}
	var lf artistBio
	if s.Lastfm != nil {
		if a, err := s.catalogArtist(id); err != nil {
			ok = false
		} else if text, link, err := s.Lastfm.ArtistBio(ctx, a.Name, lang); err != nil {
			log.Printf("bio of %s: last.fm: %v", id, err)
			ok = false
		} else {
			lf = artistBio{Text: bioText(text), Source: "lastfm", URL: link}
		}
	}
	switch {
	case lf.Text != "" && inLang(lf.Text, lang):
		return lf, ok
	case dz.Text != "":
		return dz, ok
	case lf.Text != "":
		return lf, ok
	}
	return artistBio{Source: "none"}, ok
}

// PUT /api/discover/artists/{id}/bio {text} — свой текст; пустой — скрыть описание.
func (s *Server) handlePutArtistBio(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || utf8.RuneCountInString(req.Text) > bioMaxRunes {
		writeError(w, http.StatusBadRequest, "text is up to 20000 characters")
		return
	}
	text := normalizeBio(req.Text)
	if _, err := s.DB.Exec(
		`INSERT INTO artist_bios (deezer_id, lang, text, source) VALUES ($1, '', $2, 'custom')
		 ON CONFLICT (deezer_id, lang) DO UPDATE SET text = EXCLUDED.text, updated_at = now()`, id, text,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	s.handleArtistBio(w, r)
}

// DELETE /api/discover/artists/{id}/bio — убрать свой текст: снова найденное.
func (s *Server) handleDeleteArtistBio(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM artist_bios WHERE deezer_id = $1 AND lang = ''`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reset")
		return
	}
	s.handleArtistBio(w, r)
}

// bioEditor: описание своего артиста правят владелец и админ, артиста Deezer — админ.
func (s *Server) bioEditor(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return "", false
	}
	owner := ""
	if isLocalID(id) {
		if _, err := s.catalogArtist(id); err != nil {
			writeCatalogError(w, "artist lookup failed", err)
			return "", false
		}
		owner = s.bioOwner(id)
	}
	if !viewerOf(r).mayEdit(owner) {
		writeError(w, http.StatusForbidden, "only the artist's creator or an admin can change it")
		return "", false
	}
	return id, true
}

var (
	// «Read more on Last.fm. User-contributed text is available under…» в конце.
	lastfmTail = regexp.MustCompile(`(?s)\s*<a href="https?://www\.last\.fm/[^"]*">[^<]*</a>\.?[^<]*$`)
	bioBreak   = regexp.MustCompile(`(?i)<br\s*/?>`)
	bioPara    = regexp.MustCompile(`(?i)</p\s*>`)
	bioTag     = regexp.MustCompile(`<[^>]*>`)
	bioBlank   = regexp.MustCompile(`\n{3,}`)
)

// bioText — HTML источника в простой текст: абзацы через пустую строку.
func bioText(s string) string {
	s = lastfmTail.ReplaceAllString(s, "")
	s = bioBreak.ReplaceAllString(s, "\n")
	s = bioPara.ReplaceAllString(s, "\n\n")
	s = bioTag.ReplaceAllString(s, "")
	return normalizeBio(html.UnescapeString(s))
}

func normalizeBio(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", " ").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	s = strings.TrimSpace(bioBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
	if utf8.RuneCountInString(s) > bioMaxRunes {
		s = string([]rune(s)[:bioMaxRunes])
	}
	return s
}

// inLang — текст в основном на этом языке (ru — кириллица, иначе латиница).
func inLang(s, lang string) bool {
	var cyr, lat int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case unicode.Is(unicode.Latin, r):
			lat++
		}
	}
	if lang == "ru" {
		return cyr > lat
	}
	return lat > cyr
}
