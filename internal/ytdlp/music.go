package ytdlp

// Поиск песен на YouTube Music — первый шаг FindMatches. В отличие от
// обычного поиска YouTube он отдаёт только официальные треки и у каждого —
// исполнителей, альбом и значок explicit. Только по нему можно отличить
// версию с матом от clean: на YouTube это два одинаковых ролика
// «<Artist> - Topic» с тем же названием, длиной и альбомом (Yeat «Luh
// gëek»: -yb4WxgNqTA и jdBxc61hGyA).
//
// yt-dlp так искать не умеет (ytmsearch: не поддерживается), поэтому это
// прямой запрос к внутреннему API music.youtube.com — тот же, что делает
// сайт. API неофициальный: если он сломается (поменяют формат, перестанут
// принимать clientVersion), FindMatches пишет об этом в лог и ищет как
// раньше.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// musicSearchURL — пустая строка выключает поиск на YouTube Music (тесты).
var musicSearchURL = "https://music.youtube.com/youtubei/v1/search?prettyPrint=false"

const (
	musicClientVersion = "1.20241023.01.00"
	// params фильтра «Песни»: без него в выдаче ещё клипы, альбомы и артисты.
	musicSongsFilter = "EgWKAQIIAWoQEAMQBBAJEAoQBRAREBAQFQ%3D%3D"
	musicTimeout     = 5 * time.Second
)

var musicClient = &http.Client{}

// searchMusic returns the songs YouTube Music finds for query.
func searchMusic(ctx context.Context, query string) ([]candidate, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]string{
			"clientName": "WEB_REMIX", "clientVersion": musicClientVersion, "hl": "en", "gl": "US",
		}},
		"query":  query,
		"params": musicSongsFilter,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, musicTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, musicSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	resp, err := musicClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music search: HTTP %d", resp.StatusCode)
	}
	return parseMusicSearch(resp.Body)
}

type musicRun struct {
	Text               string `json:"text"`
	NavigationEndpoint struct {
		BrowseEndpoint struct {
			Configs struct {
				Music struct {
					PageType string `json:"pageType"`
				} `json:"browseEndpointContextMusicConfig"`
			} `json:"browseEndpointContextSupportedConfigs"`
		} `json:"browseEndpoint"`
	} `json:"navigationEndpoint"`
}

func (r musicRun) pageType() string {
	return r.NavigationEndpoint.BrowseEndpoint.Configs.Music.PageType
}

type musicItem struct {
	FlexColumns []struct {
		Column struct {
			Text struct {
				Runs []musicRun `json:"runs"`
			} `json:"text"`
		} `json:"musicResponsiveListItemFlexColumnRenderer"`
	} `json:"flexColumns"`
	PlaylistItemData struct {
		VideoID string `json:"videoId"`
	} `json:"playlistItemData"`
	Badges []struct {
		Badge struct {
			Icon struct {
				IconType string `json:"iconType"`
			} `json:"icon"`
		} `json:"musicInlineBadgeRenderer"`
	} `json:"badges"`
	// "MUSIC_ITEM_RENDERER_DISPLAY_POLICY_GREY_OUT" — серый, не играет.
	DisplayPolicy string `json:"musicItemRendererDisplayPolicy"`
}

// parseMusicSearch pulls the songs out of a search response. The list sits
// deep in tabs/shelves whose layout YouTube changes now and then, so the
// items are looked up by their renderer name anywhere in the tree.
func parseMusicSearch(r io.Reader) ([]candidate, error) {
	var tree any
	if err := json.NewDecoder(r).Decode(&tree); err != nil {
		return nil, fmt.Errorf("youtube music search: %w", err)
	}
	var raw []any
	var collect func(any)
	collect = func(o any) {
		switch v := o.(type) {
		case map[string]any:
			if item, ok := v["musicResponsiveListItemRenderer"]; ok {
				raw = append(raw, item)
				return
			}
			for _, x := range v {
				collect(x)
			}
		case []any:
			for _, x := range v {
				collect(x)
			}
		}
	}
	collect(tree)
	if len(raw) == 0 {
		// И «ничего не нашлось», и сменившийся формат: ищем по-старому.
		return nil, noMatch("youtube music: no songs in the response")
	}
	var out []candidate
	for _, x := range raw {
		b, _ := json.Marshal(x)
		var item musicItem
		if json.Unmarshal(b, &item) != nil {
			continue
		}
		if c, ok := item.candidate(); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// candidate reads one song: column 0 is the title, column 1 is
// "artists • album • 2:56" (artists split by ", " and " & ").
func (item musicItem) candidate() (candidate, bool) {
	if item.PlaylistItemData.VideoID == "" || len(item.FlexColumns) < 2 ||
		strings.HasSuffix(item.DisplayPolicy, "GREY_OUT") {
		return candidate{}, false
	}
	var title strings.Builder
	for _, r := range item.FlexColumns[0].Column.Text.Runs {
		title.WriteString(r.Text)
	}
	c := candidate{ID: item.PlaylistItemData.VideoID, Title: title.String(), Music: true}
	// Разделитель — отдельный кусок " • "; внутри имён бывает и «•» (time•bender).
	part := 0
	for _, r := range item.FlexColumns[1].Column.Text.Runs {
		switch {
		case r.Text == " • ":
			part++
		case part == 0 && r.Text != ", " && r.Text != " & ":
			c.Artists = append(c.Artists, r.Text)
		case r.pageType() == "MUSIC_PAGE_TYPE_ALBUM":
			c.Album = r.Text
		default:
			if ms, ok := parseClock(r.Text); ok {
				c.DurationMs = ms
			}
		}
	}
	c.Channel = strings.Join(c.Artists, ", ")
	for _, b := range item.Badges {
		if b.Badge.Icon.IconType == "MUSIC_EXPLICIT_BADGE" {
			c.Explicit = true
		}
	}
	return c, c.DurationMs > 0
}

// parseClock reads "2:56" or "1:02:03" as milliseconds.
func parseClock(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	sec := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, false
		}
		sec = sec*60 + n
	}
	return sec * 1000, true
}

// rankMusic returns the songs that plausibly are the Deezer recording,
// best first. Like rankCandidates for "- Topic" uploads (the title is just
// the song's name: words must match both ways, no other-version markers,
// duration within toleranceMs), plus:
//
//   - one of the Deezer artists must be among the song's artists — YouTube
//     Music lists covers and tributes under their own artists; a name
//     written differently (another script) finds nothing here and falls
//     back to the plain search;
//   - the same album as on Deezer ranks higher (album vs. deluxe vs. the
//     single with another edit);
//   - explicit must agree with Deezer — a mismatch isn't dropped, since
//     labels don't always mark it, but ranks below every match.
func rankMusic(candidates []candidate, want Want, toleranceMs int) ([]candidate, error) {
	wantTitle := normalize(want.Title)
	wantTokens := strings.Fields(wantTitle)
	coreTokens := strings.Fields(normalize(coreTitle(want.Title)))
	var wantArtists []string
	for _, a := range append([]string{want.Artist}, want.OtherArtists...) {
		if a = normalize(a); a != "" {
			wantArtists = append(wantArtists, a)
		}
	}
	wantAlbum := normalize(want.Album)

	type scored struct {
		c     candidate
		score float64
	}
	var ok []scored
	for _, c := range candidates {
		diffMs := abs(c.DurationMs - want.DurationMs)
		if diffMs > toleranceMs {
			continue
		}
		title := normalize(c.Title)
		if wordShare(title, coreTokens) < 0.5 || wordShare(wantTitle, strings.Fields(normalize(coreTitle(c.Title)))) <= 0.5 {
			continue
		}
		if otherVersionMarker(title, wantTitle) != "" {
			continue
		}
		artists := normalize(c.Channel)
		if !containsAny(artists, wantArtists) {
			continue
		}
		score := 2*wordShare(title, wantTokens) - float64(diffMs)/float64(toleranceMs+1)
		if wantAlbum != "" && normalize(c.Album) == wantAlbum {
			score++
		}
		if want.Explicit != nil && c.Explicit != *want.Explicit {
			score -= 10
		}
		ok = append(ok, scored{c, score})
	}
	if len(ok) == 0 {
		return nil, noMatch("youtube music: no song with a matching title, artist and duration within %dms", toleranceMs)
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].score > ok[j].score })
	out := make([]candidate, len(ok))
	for i, s := range ok {
		out[i] = s.c
	}
	return out, nil
}

func containsAny(s string, phrases []string) bool {
	for _, p := range phrases {
		if containsWord(s, p) {
			return true
		}
	}
	return false
}

// findOnMusic is the YouTube Music step of FindMatches.
func findOnMusic(ctx context.Context, want Want, toleranceMs int) ([]string, error) {
	if musicSearchURL == "" {
		return nil, errMusicDisabled
	}
	candidates, err := searchMusic(ctx, want.Artist+" "+want.Title)
	if err != nil {
		return nil, err
	}
	ranked, err := rankMusic(candidates, want, toleranceMs)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ranked))
	for i, c := range ranked {
		ids[i] = c.ID
	}
	return ids, nil
}

var errMusicDisabled = errors.New("youtube music search is off")
