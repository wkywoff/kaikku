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

var execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = toolEnv(os.Environ())
	return cmd
}

func toolEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "DATABASE_URL", name == "LASTFM_API_KEY", name == "LRCRED_CONTACT",
			strings.HasSuffix(name, "_SECRET"), strings.HasSuffix(name, "_PASSWORD"), strings.HasSuffix(name, "_TOKEN"):
			continue
		}
		out = append(out, kv)
	}
	return out
}

type candidate struct {
	ID         string
	Title      string
	Channel    string
	DurationMs int
	Music      bool
	Artists    []string
	Album      string
	Explicit   bool
}

type ytDlpEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Uploader string  `json:"uploader"`
	Duration float64 `json:"duration"`
}

func wrapWithStderr(err error) error {
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		return classify(fmt.Errorf("%w: %s", err, stderr), stderr)
	}
	return err
}

func parseEntries(jsonLines string) []candidate {
	var results []candidate
	for _, line := range strings.Split(strings.TrimSpace(jsonLines), "\n") {
		if line == "" {
			continue
		}
		var e ytDlpEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
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

type Want struct {
	Artist             string
	OtherArtists       []string
	Title              string
	Album              string
	DurationMs         int
	Explicit           *bool
	VideoID            string
	SearchIfVideoFails bool
}

func NullBool(b sql.NullBool) *bool {
	if !b.Valid {
		return nil
	}
	return &b.Bool
}

var ErrNoMatch = errors.New("no matching video on youtube")

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
				if i > 0 {
					log.Printf("youtube: %q found by query %d (%q), last search %.1fs", want.Title, i+1, q, time.Since(started).Seconds())
				}
				return ids, nil
			}
			err = fmt.Errorf("%q: %w", q, err)
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil || errors.Is(err, ErrBlocked) {
			break
		}
	}
	return nil, firstErr
}

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
	out, err := ytdlpOutput(ctx, "-j", "--no-warnings", "--ignore-errors", "--flat-playlist", query)
	candidates := parseEntries(string(out))
	if err != nil && len(candidates) == 0 {
		return nil, fmt.Errorf("yt-dlp search: %w", wrapWithStderr(err))
	}
	return candidates, nil
}

var otherVersionMarkers = []string{
	"live", "cover", "karaoke", "instrumental", "remix", "sped up", "speed up",
	"slowed", "nightcore", "8d", "reverb", "acapella", "a cappella", "1 hour",
	"drumless", "acoustic", "piano", "demo", "extended", "mashup", "bass boosted",
	"lofi", "lo fi", "tribute", "parody",
}

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
			if share < 0.5 || wordShare(wantTitle, strings.Fields(normalize(coreTitle(c.Title)))) <= 0.5 {
				continue
			}
		} else if share <= 0.5 {
			continue
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

func pickBestMatch(candidates []candidate, want Want, toleranceMs int) (string, error) {
	ranked, err := rankCandidates(candidates, want, toleranceMs)
	if err != nil {
		return "", err
	}
	return ranked[0].ID, nil
}

func coreTitle(title string) string {
	for _, sep := range []string{" (", " [", " - "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	return title
}

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

func otherVersionMarker(title, wantTitle string) string {
	for _, m := range otherVersionMarkers {
		if containsWord(title, m) && !containsWord(wantTitle, m) {
			return m
		}
	}
	return ""
}

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

var lookalikes = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'і': 'i', 'ј': 'j', 'к': 'k', 'м': 'm', 'н': 'h',
	'о': 'o', 'р': 'p', 'с': 'c', 'ѕ': 's', 'т': 't', 'у': 'y', 'х': 'x',
}

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

func containsWord(s, phrase string) bool {
	return strings.Contains(" "+s+" ", " "+phrase+" ")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

const quickClientArgs = "youtube:player_client=visionos"

func Download(ctx context.Context, videoID, stagingDir, basename string) (path string, err error) {
	outputTemplate := filepath.Join(stagingDir, basename+".%(ext)s")
	watchURL := "https://www.youtube.com/watch?v=" + videoID

	pattern := filepath.Join(stagingDir, basename+".*")
	defer func() {
		if err != nil {
			leftovers, _ := filepath.Glob(pattern)
			for _, f := range leftovers {
				os.Remove(f)
			}
		}
	}()

	type run struct {
		args   []string
		signIn bool
	}
	quick := run{[]string{"--extractor-args", quickClientArgs, "-f", "140"}, false}
	plain := run{[]string{"-f", "140/bestaudio"}, false}
	signedIn := run{plain.args, true}
	runs := []run{quick, plain, plain}
	if cookiesFirst() {
		runs = []run{signedIn, signedIn}
	}
	for i := 0; i < len(runs); i++ {
		r := runs[i]
		args := append([]string{"--no-warnings"}, r.args...)
		cmd, done := command(ctx, r.signIn, append(args, "-o", outputTemplate, watchURL)...)
		out, runErr := cmd.CombinedOutput()
		done()
		if runErr == nil {
			break
		}
		output := string(out)
		switch {
		case ctx.Err() != nil || !needsSignIn(output):
		case !r.signIn && signInNeeded(output):
			runs = append(runs[:i+1], signedIn)
		case r.signIn || i > 0:
			runs = runs[:i+1]
		}
		if i == len(runs)-1 || ctx.Err() != nil {
			err := fmt.Errorf("yt-dlp download: %w (output: %s)", runErr, truncate(output, 1500))
			return "", classify(err, output)
		}
		if i == 0 && !r.signIn {
			log.Printf("yt-dlp %s: %s failed, trying the default clients: %s", videoID, quickClientArgs, truncate(strings.TrimSpace(output), 300))
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
