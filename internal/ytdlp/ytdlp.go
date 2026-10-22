// Package ytdlp shells out to the yt-dlp binary (os/exec, same pattern as
// ffmpeg in internal/ingest) to locate and download audio for a track whose
// metadata already came from Deezer. YouTube is only the audio source here —
// titles/artists are never taken from it.
//
// Only plain `ytsearchN:` and `https://www.youtube.com/watch?v=` are used:
// `ytmsearch:` is an unsupported scheme and music.youtube.com URLs are
// mishandled by yt-dlp (verified against real runs).
//
// yt-dlp needs a JS runtime (Deno) for reliable extraction and has to be
// updated regularly — YouTube breaks extractors often.
package ytdlp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// execCommand is exec.CommandContext; tests swap it for a fake yt-dlp.
var execCommand = exec.CommandContext

type candidate struct {
	ID         string
	Title      string
	Channel    string // у YouTube Music — исполнители через запятую
	DurationMs int
	// Только у песен с YouTube Music (searchMusic).
	Music    bool
	Artists  []string
	Album    string
	Explicit bool
}

type ytDlpEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Uploader string  `json:"uploader"`
	Duration float64 `json:"duration"`
}

// wrapWithStderr extracts yt-dlp's actual error message. cmd.Output() only
// returns stdout — on failure the error is just "exit status 1" unless we
// pull the process's stderr out of the ExitError (populated automatically
// when cmd.Stderr is nil). Without this, bot detection, an outdated yt-dlp
// and network issues all look the same.
func wrapWithStderr(err error) error {
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}

// parseEntries parses yt-dlp's `-j` output (one JSON object per line),
// skipping malformed lines. Pure, so it's unit-testable without the binary.
func parseEntries(jsonLines string) []candidate {
	var results []candidate
	for _, line := range strings.Split(strings.TrimSpace(jsonLines), "\n") {
		if line == "" {
			continue
		}
		var e ytDlpEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // одна кривая строка не должна валить весь результат
		}
		results = append(results, e.candidate())
	}
	return results
}

func (e ytDlpEntry) candidate() candidate {
	channel := e.Channel
	if channel == "" {
		channel = e.Uploader
	}
	return candidate{
		ID:         e.ID,
		Title:      e.Title,
		Channel:    channel,
		DurationMs: int(e.Duration * 1000),
	}
}

// Want is the Deezer-side description of the recording to find.
type Want struct {
	Artist       string
	OtherArtists []string // фиты/соавторы, без Artist
	Title        string
	Album        string
	DurationMs   int
	// Explicit — метка Deezer: версия с матом или clean (nil — неизвестно,
	// задачи до 0037). По ней выбирается песня на YouTube Music.
	Explicit *bool
	// VideoID — видео, которое брать без поиска: ссылка от админа или то,
	// с которого трек уже качали (его файл удалили как неслушаемый).
	VideoID string
	// SearchIfVideoFails: VideoID не открылся (видео удалили) — искать как
	// обычно. Для ссылки админа — нет: он выбрал именно это видео.
	SearchIfVideoFails bool
}

// NullBool is Want.Explicit from a nullable column.
func NullBool(b sql.NullBool) *bool {
	if !b.Valid {
		return nil
	}
	return &b.Bool
}

// ErrNoMatch — на YouTube не нашлось подходящего видео (а не сбой сети или
// yt-dlp): errors.Is(err, ErrNoMatch).
var ErrNoMatch = errors.New("no matching video on youtube")

// noMatchError — текст ошибки как раньше (он сохраняется в задаче), но
// errors.Is узнаёт в ней ErrNoMatch.
type noMatchError struct{ msg string }

func (e *noMatchError) Error() string        { return e.msg }
func (e *noMatchError) Is(target error) bool { return target == ErrNoMatch }

func noMatch(format string, args ...any) error {
	return &noMatchError{msg: fmt.Sprintf(format, args...)}
}

var (
	videoIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	videoURLRe = regexp.MustCompile(`(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|embed/|live/)|youtu\.be/)([A-Za-z0-9_-]{11})`)
)

// VideoIDFromURL — ID видео из ссылки YouTube (youtube.com/watch?v=,
// music.youtube.com, youtu.be/, /shorts/) или из самого ID.
func VideoIDFromURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if videoIDRe.MatchString(s) {
		return s, true
	}
	if m := videoURLRe.FindStringSubmatch(s); m != nil {
		return m[1], true
	}
	return "", false
}

// FindMatches searches YouTube and returns qualifying video IDs, best first,
// so the caller can fall back to the next one if a download fails.
//
// First YouTube Music (findOnMusic, rankMusic): official songs only, with
// artists, album and the explicit badge. If it finds nothing that fits or
// fails, the plain YouTube search (rankCandidates) tries queries in order
// until one yields a match:
//
//   - "artist title";
//   - "artist featured title" — with all of the track's artists; some
//     tracks are only found this way ("тихийпричал туман" finds nothing,
//     "тихийпричал Kivirin туман" finds the official Topic upload);
//   - "artist title topic" — the official audio upload, when the music
//     video and fan re-uploads push it out of the plain results ("Nightvox BIG
//     MISS (prod. by velloe)" finds only the longer video, with "topic"
//     the 134 s audio from the album);
//   - "artist title audio" — surfaces re-uploads of the album audio when the
//     official upload is region-blocked or a different edit;
//   - "artist album" — YouTube's search sometimes can't find a track by its
//     own title at all (e.g. one-word titles like THEONEANDONLYHUNGER) but
//     lists it among the album's uploads.
//
// Search is flat (--flat-playlist): one results page, ~1.5 s for 15
// candidates instead of ~13 s for full extraction of each; the page already
// carries duration and channel.
func FindMatches(ctx context.Context, want Want, resultCount, toleranceMs int) ([]string, error) {
	if resultCount <= 0 {
		resultCount = 15
	}
	ids, err := findOnMusic(ctx, want, toleranceMs)
	if err == nil {
		return ids, nil
	}
	if !errors.Is(err, errMusicDisabled) {
		log.Printf("youtube music: %q: %v, trying youtube search", want.Title, err)
	}
	if ctx.Err() != nil {
		return nil, err
	}
	var firstErr error
	for i, q := range searchQueries(want) {
		started := time.Now()
		candidates, err := search(ctx, fmt.Sprintf("ytsearch%d:%s", resultCount, q))
		if err == nil {
			var ranked []candidate
			ranked, err = rankCandidates(candidates, want, toleranceMs)
			if err == nil {
				ids := make([]string, len(ranked))
				for i, c := range ranked {
					ids[i] = c.ID
				}
				if i > 0 { // каждый лишний поиск — ещё ~0.5 с до звука
					log.Printf("youtube: %q found by query %d (%q), last search %.1fs", want.Title, i+1, q, time.Since(started).Seconds())
				}
				return ids, nil
			}
			err = fmt.Errorf("%q: %w", q, err)
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, firstErr
}

// searchQueries is the query cascade of FindMatches.
func searchQueries(want Want) []string {
	queries := []string{want.Artist + " " + want.Title}
	if len(want.OtherArtists) > 0 {
		queries = append(queries, want.Artist+" "+strings.Join(want.OtherArtists, " ")+" "+want.Title)
	}
	queries = append(queries, want.Artist+" "+want.Title+" topic", want.Artist+" "+want.Title+" audio")
	if want.Album != "" && normalize(want.Album) != normalize(want.Title) {
		queries = append(queries, want.Artist+" "+want.Album)
	}
	return queries
}

func search(ctx context.Context, query string) ([]candidate, error) {
	if w := warm.Load(); w != nil {
		candidates, err := w.search(ctx, query)
		if !errors.Is(err, errWarmUnavailable) {
			return candidates, err
		}
	}
	// --ignore-errors: один недоступный кандидат (гео/возраст/удалён) иначе
	// обрывает весь поиск. yt-dlp при этом всё равно выходит с кодом 1, так
	// что ошибка фатальна, только если не распарсился ни один кандидат.
	cmd := execCommand(ctx, "yt-dlp", "-j", "--no-warnings", "--ignore-errors", "--flat-playlist", query)
	out, err := cmd.Output()
	candidates := parseEntries(string(out))
	if err != nil && len(candidates) == 0 {
		return nil, fmt.Errorf("yt-dlp search: %w", wrapWithStderr(err))
	}
	return candidates, nil
}

// Слова, которые в названии ролика почти всегда означают другую запись, чем
// студийная версия из Deezer, — если только они не стоят в самом
// Deezer-тайтле ("Song (Live)", "Song - Remix").
var otherVersionMarkers = []string{
	"live", "cover", "karaoke", "instrumental", "remix", "sped up", "speed up",
	"slowed", "nightcore", "8d", "reverb", "acapella", "a cappella", "1 hour",
	"drumless", "acoustic", "piano", "demo", "extended", "mashup", "bass boosted",
	"lofi", "lo fi", "tribute", "parody",
}

// rankCandidates returns candidates that plausibly are the Deezer
// recording, best first. Duration alone can't tell the studio recording
// from a karaoke or live upload of the same length — or, for album-wide
// queries, from another track of the same album — so a candidate must:
//
//   - be within toleranceMs of the expected duration;
//   - contain most (more than half) of the words of the core title (the
//     part before "(" / " - ", so "(feat. …)" or "(Radio Edit)" can't fail
//     it) — half let "LIKE A PUNK" pass for "PUNK TACTICS";
//   - "<Artist> - Topic" uploads, whose title is just the song's name, may
//     have only half of them (the name in the original script: "Love Love
//     妄想パレード" for "Love Love Moso Parade"), but then also the other way
//     round: most of their words must be in the Deezer title, otherwise
//     it's another song of the same artist;
//   - not be marked as another version ("live", "cover", "sped up", …)
//     unless the Deezer title says so too.
//
// Ranking: auto-generated "<Artist> - Topic" channels (official audio),
// then the artist's own channel, then title-word overlap, then duration.
//
// Returns an error rather than a bad guess when nothing qualifies — wrong
// audio on a track is worse than a failed job the person can retry.
func rankCandidates(candidates []candidate, want Want, toleranceMs int) ([]candidate, error) {
	if len(candidates) == 0 {
		return nil, noMatch("youtube search returned no results")
	}
	wantTitle := normalize(want.Title)
	wantTokens := strings.Fields(wantTitle)
	coreTokens := strings.Fields(normalize(coreTitle(want.Title)))
	wantArtist := normalize(want.Artist)

	type scored struct {
		c     candidate
		score float64
	}
	var ok []scored
	closest := candidates[0]
	var rejected []string
	for _, c := range candidates {
		diffMs := abs(c.DurationMs - want.DurationMs)
		if diffMs < abs(closest.DurationMs-want.DurationMs) {
			closest = c
		}
		if diffMs > toleranceMs {
			continue
		}

		title := normalize(c.Title)
		share := wordShare(title, coreTokens)
		topic := strings.HasSuffix(strings.TrimSpace(c.Channel), "- Topic")
		if topic {
			// В названии только песня: лишние слова — другая песня.
			if share < 0.5 || wordShare(wantTitle, strings.Fields(normalize(coreTitle(c.Title)))) <= 0.5 {
				continue
			}
		} else if share <= 0.5 {
			continue // другая песня той же длины (типично для поиска по альбому)
		}
		if m := otherVersionMarker(title, wantTitle); m != "" {
			rejected = append(rejected, fmt.Sprintf("%q (%s)", c.Title, m))
			continue
		}
		channel := normalize(c.Channel)
		score := 0.0
		if topic {
			score += 3
		}
		if wantArtist != "" && strings.Contains(channel, wantArtist) {
			score++
		}
		score += 2 * wordShare(title, wantTokens)
		score -= float64(diffMs) / float64(toleranceMs+1)
		ok = append(ok, scored{c, score})
	}
	if len(ok) == 0 && len(rejected) > 0 {
		return nil, noMatch("only other versions within %dms tolerance: %s", toleranceMs, strings.Join(rejected, ", "))
	}
	if len(ok) == 0 {
		return nil, noMatch(
			"no result with a matching title within %dms tolerance (closest by duration: %q, %dms)",
			toleranceMs, closest.Title, closest.DurationMs,
		)
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].score > ok[j].score })
	out := make([]candidate, len(ok))
	for i, s := range ok {
		out[i] = s.c
	}
	return out, nil
}

// pickBestMatch is rankCandidates' top choice.
func pickBestMatch(candidates []candidate, want Want, toleranceMs int) (string, error) {
	ranked, err := rankCandidates(candidates, want, toleranceMs)
	if err != nil {
		return "", err
	}
	return ranked[0].ID, nil
}

// coreTitle drops version/feature suffixes: "Get Lucky (Radio Edit - feat.
// X)" -> "Get Lucky", "Song - Remastered 2011" -> "Song".
func coreTitle(title string) string {
	for _, sep := range []string{" (", " [", " - "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	return title
}

// wordShare is the fraction of tokens present in normalized s (1 for none).
func wordShare(s string, tokens []string) float64 {
	if len(tokens) == 0 {
		return 1
	}
	hit := 0
	for _, tok := range tokens {
		if containsWord(s, tok) {
			hit++
		}
	}
	return float64(hit) / float64(len(tokens))
}

// otherVersionMarker returns the first marker present in the video title but
// not in the Deezer title, or "".
func otherVersionMarker(title, wantTitle string) string {
	for _, m := range otherVersionMarkers {
		if containsWord(title, m) && !containsWord(wantTitle, m) {
			return m
		}
	}
	return ""
}

// normalize lowercases and turns everything that isn't a letter or digit
// into single spaces, so "Song (Sped-Up)" and "song sped up" compare equal.
// Cyrillic look-alikes in mixed-script words become Latin (foldLookalikes).
func normalize(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return foldLookalikes(strings.TrimSpace(b.String()))
}

// Кириллические буквы (после ToLower), которые выглядят как латинские — в
// строчном или в заглавном виде.
var lookalikes = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'і': 'i', 'ј': 'j', 'к': 'k', 'м': 'm', 'н': 'h',
	'о': 'o', 'р': 'p', 'с': 'c', 'ѕ': 's', 'т': 't', 'у': 'y', 'х': 'x',
}

// foldLookalikes переводит кириллических двойников в латиницу в словах, где
// смешаны обе азбуки: на YouTube название пишут «оverblood» с кириллической
// «о», и оно не совпадает с «overblood» из Deezer. Обе стороны сравнения
// проходят через normalize, поэтому смешанное и в Deezer («KICKЭСС»)
// совпадает с таким же на YouTube. Чисто кириллические слова не трогаются.
func foldLookalikes(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		var latin, cyrillic bool
		for _, r := range w {
			latin = latin || unicode.Is(unicode.Latin, r)
			cyrillic = cyrillic || unicode.Is(unicode.Cyrillic, r)
		}
		if latin && cyrillic {
			words[i] = strings.Map(func(r rune) rune {
				if l, ok := lookalikes[r]; ok {
					return l
				}
				return r
			}, w)
		}
	}
	return strings.Join(words, " ")
}

// containsWord reports whether phrase occurs in normalized s on word
// boundaries ("live" must not match "alive").
func containsWord(s, phrase string) bool {
	return strings.Contains(" "+s+" ", " "+phrase+" ")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// quickClientArgs: клиент YouTube, ссылки которого не требуют JS-проверки
// плеера. Штатные клиенты yt-dlp 2026.10.21 (tv_samsung + visionos) решают
// её в Deno — ~3 с процессора на ролик на 1 vCPU (замер 2026-10-22);
// visionos один отдаёт формат 140 без неё. helper.py делает так же.
const quickClientArgs = "youtube:player_client=visionos"

// Download fetches the audio for videoID into stagingDir as
// "<basename>.<ext>". Format 140 (AAC ~128k in m4a) is preferred: AAC plays
// everywhere including iOS Safari, so internal/ingest only has to remux it,
// not re-encode. If 140 is missing, any best audio (then encoded). Returns
// the actual path written, found by globbing since ext isn't known upfront.
func Download(ctx context.Context, videoID, stagingDir, basename string) (path string, err error) {
	outputTemplate := filepath.Join(stagingDir, basename+".%(ext)s")
	watchURL := "https://www.youtube.com/watch?v=" + videoID

	pattern := filepath.Join(stagingDir, basename+".*")
	defer func() {
		if err != nil { // недокачанные .part/.ytdl не должны копиться в staging
			leftovers, _ := filepath.Glob(pattern)
			for _, f := range leftovers {
				os.Remove(f)
			}
		}
	}()

	// Сначала быстрый клиент (quickClientArgs), только формат 140. Не вышло —
	// штатные клиенты, дважды: YouTube периодически отдаёт 403 на сам поток,
	// хотя повторный запуск (с новой экстракцией ссылок) проходит.
	attempts := [][]string{
		{"--extractor-args", quickClientArgs, "-f", "140"},
		{"-f", "140/bestaudio"},
		{"-f", "140/bestaudio"},
	}
	for i, extra := range attempts {
		args := append([]string{"--no-warnings"}, extra...)
		cmd := execCommand(ctx, "yt-dlp", append(args, "-o", outputTemplate, watchURL)...)
		out, runErr := cmd.CombinedOutput()
		if runErr == nil {
			break
		}
		if i == len(attempts)-1 || ctx.Err() != nil {
			return "", fmt.Errorf("yt-dlp download: %w (output: %s)", runErr, truncate(string(out), 1500))
		}
		if i == 0 {
			log.Printf("yt-dlp %s: %s failed, trying the default clients: %s", videoID, quickClientArgs, truncate(strings.TrimSpace(string(out)), 300))
		}
		leftovers, _ := filepath.Glob(pattern)
		for _, f := range leftovers {
			os.Remove(f)
		}
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("globbing for downloaded file: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("yt-dlp reported success but no output file found for %q", basename)
	}
	return matches[0], nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
