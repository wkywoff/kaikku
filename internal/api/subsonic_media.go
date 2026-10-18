package api

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/lyrics"
)

// Звук, картинки, скробблинг и тексты для приложений.

func init() {
	subsonicRoute("stream", subsonicStream)
	subsonicRoute("download", subsonicStream)
	subsonicRoute("getCoverArt", subsonicCoverArt)
	subsonicRoute("scrobble", subsonicScrobble)
	subsonicRoute("getLyricsBySongId", subsonicLyricsBySongID)
	subsonicRoute("getLyrics", subsonicLyrics)
}

// stream — та же ссылка, что у плеера сайта (handleDeezerStreamURL):
// скачанный трек отдаёт nginx, нескачанный — API потоком, пока качает.
// Перекодирования нет: всегда AAC .m4a, maxBitRate и format не важны.
func subsonicStream(c *subCtx) error {
	id, err := c.catalogParam("id", subTrack)
	if err != nil {
		return err
	}
	var link struct {
		URL string `json:"url"`
	}
	if err := c.call(c.s.handleDeezerStreamURL, "GET", "/", map[string]string{"id": id}, nil, &link); err != nil {
		return err
	}
	if link.URL == "" {
		return subFail(subErrGeneric, "no audio")
	}
	http.Redirect(c.w, c.r, link.URL, http.StatusFound)
	return nil
}

// --- обложки ---

// getCoverArt: id — альбом, трек (обложка его альбома), артист или
// плейлист; size — сторона квадрата. Картинку Deezer отдаёт наш прокси
// (images.go), свою — covers.go.
func subsonicCoverArt(c *subCtx) error {
	id := c.param("id")
	if id == "" {
		return subMissing("id")
	}
	u, direct := coverFromID(id)
	if !direct {
		var err error
		if u, err = c.coverURL(id); err != nil {
			return err
		}
	}
	if u == "" {
		// Нет картинки — заглушка, а не битая картинка в приложении.
		placeholder(c.w, strings.HasPrefix(id, subArtist))
		return nil
	}
	size := c.intParam("size", 0, 4000)
	if kind, hash, file, ok := deezerImageAt(u, size); ok {
		r := c.r.Clone(c.r.Context())
		r.SetPathValue("kind", kind)
		r.SetPathValue("hash", hash)
		r.SetPathValue("file", file)
		c.s.handleDeezerImage(c.w, r)
		return nil
	}
	if key, ok := strings.CutPrefix(u, "/media/"); ok && coverKeyRe.MatchString(key) && c.s.StorageDir != "" {
		if px := fitSize(size, []int{64, 200, 300, 320, 640}); px > 0 {
			r := c.r.Clone(c.r.Context())
			r.SetPathValue("px", strconv.Itoa(px))
			r.SetPathValue("key", key)
			c.s.handleSizedCover(c.w, r)
			return nil
		}
		c.w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(c.w, c.r, filepath.Join(c.s.StorageDir, key))
		return nil
	}
	http.Redirect(c.w, c.r, u, http.StatusFound)
	return nil
}

// coverURL — картинка объекта, какой её видят на сайте (с правками).
func (c *subCtx) coverURL(id string) (string, error) {
	kind, raw := parseSubID(id)
	switch {
	case kind == subAlbum && validCatalogID(raw):
		if a := c.s.albumCard(raw); a != nil {
			return a.CoverURL, nil
		}
	case kind == subTrack && validCatalogID(raw):
		t, err := c.s.catalogTrack(viewerOf(c.r), raw)
		if err != nil {
			return "", subFail(subErrNotFound, "song not found")
		}
		if t.AlbumID != "" {
			if a := c.s.albumCard(t.AlbumID); a != nil {
				return a.CoverURL, nil
			}
		}
		return t.CoverURL, nil
	case kind == subArtist && validCatalogID(raw):
		var a deezer.Artist
		if isLocalID(raw) {
			p, err := c.s.catalogArtist(raw)
			if err != nil {
				return "", subFail(subErrNotFound, "artist not found")
			}
			a = *p
		} else {
			var err error
			if a, err = c.s.artistInfo(raw); err != nil {
				return "", err
			}
		}
		c.s.overlayFor([]*deezer.Artist{&a}, nil, nil)
		return a.PictureURL, nil
	case kind == subPlaylist:
		p, err := c.s.visiblePlaylist(c.userID, raw)
		if err != nil {
			return "", subFail(subErrNotFound, "playlist not found")
		}
		if len(p.Covers) > 0 {
			return p.Covers[0], nil
		}
		return "", nil
	}
	return "", subFail(subErrNotFound, "no such item: "+id)
}

var deezerImageRe = regexp.MustCompile(`^https://(?:cdn-images|e-cdns-images)\.dzcdn\.net/images/([a-z]+)/([0-9a-f]{32})/[0-9]+x[0-9]+-(.+)$`)

// deezerImageAt — путь картинки Deezer для прокси (images.go) в размере
// не меньше size из тех, что он отдаёт; 0 — самый большой.
func deezerImageAt(u string, size int) (kind, hash, file string, ok bool) {
	m := deezerImageRe.FindStringSubmatch(u)
	if m == nil {
		return "", "", "", false
	}
	px := fitSize(size, []int{56, 64, 200, 250, 300, 320, 500, 640, 1000})
	if px == 0 {
		px = 1000
	}
	return m[1], m[2], fmt.Sprintf("%dx%d-%s", px, px, m[3]), true
}

// fitSize — наименьший из sizes (по возрастанию), не меньше size; 0 —
// size не задан или больше всех.
func fitSize(size int, sizes []int) int {
	if size <= 0 {
		return 0
	}
	for _, px := range sizes {
		if px >= size {
			return px
		}
	}
	return 0
}

// --- прослушивания ---

// scrobble: submission=false — трек начал играть («сейчас играет» в
// Last.fm), иначе — дослушан: в историю (миксы, итоги года) и в Last.fm.
// time — когда начал играть, мс.
func subsonicScrobble(c *subCtx) error {
	ids := c.params("id")
	if len(ids) == 0 {
		return subMissing("id")
	}
	times := c.params("time")
	submission := c.param("submission") != "false"
	for i, id := range ids {
		kind, raw := parseSubID(id)
		if kind != subTrack || !validCatalogID(raw) {
			return subFail(subErrNotFound, "no such song: "+id)
		}
		if !submission {
			if err := c.call(c.s.handleNowPlaying, "POST", "/", nil, map[string]string{"deezer_id": raw}, nil); err != nil {
				return err
			}
			continue
		}
		started := time.Now().Unix()
		if i < len(times) {
			if ms, err := strconv.ParseInt(times[i], 10, 64); err == nil && ms > 0 {
				started = ms / 1000
			}
		}
		if err := c.call(c.s.handleRecordPlay, "POST", "/", nil, map[string]any{"deezer_id": raw}, nil); err != nil {
			return err
		}
		if err := c.call(c.s.handleScrobble, "POST", "/", nil, map[string]any{"deezer_id": raw, "started_at": started}, nil); err != nil {
			return err
		}
	}
	return c.ok("", nil)
}

// --- тексты ---

type subLyricsLine struct {
	Start *int64 `xml:"start,attr,omitempty" json:"start,omitempty"`
	Value string `xml:",chardata" json:"value"`
}

type subStructuredLyrics struct {
	DisplayArtist string          `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	DisplayTitle  string          `xml:"displayTitle,attr,omitempty" json:"displayTitle,omitempty"`
	Lang          string          `xml:"lang,attr" json:"lang"`
	Synced        bool            `xml:"synced,attr" json:"synced"`
	Lines         []subLyricsLine `xml:"line" json:"line"`
}

// lyricsOf — текст трека, как на сайте (handleLyrics); nil — его нет.
func (c *subCtx) lyricsOf(id string) (*lyrics.Lyrics, error) {
	var out struct {
		Lyrics *lyrics.Lyrics `json:"lyrics"`
	}
	err := c.call(c.s.handleLyrics, "GET", "/", map[string]string{"id": id}, nil, &out)
	return out.Lyrics, err
}

// getLyricsBySongId (OpenSubsonic songLyrics) — с временем строк, если оно есть.
func subsonicLyricsBySongID(c *subCtx) error {
	id, err := c.catalogParam("id", subTrack)
	if err != nil {
		return err
	}
	l, err := c.lyricsOf(id)
	if err != nil {
		// Источники не ответили — для приложения текста просто нет.
		log.Printf("subsonic lyrics %s: %v", id, err)
		l = nil
	}
	type list struct {
		Lyrics []subStructuredLyrics `xml:"structuredLyrics" json:"structuredLyrics,omitempty"`
	}
	if l == nil || len(l.Lines) == 0 {
		return c.ok("lyricsList", list{})
	}
	song, _ := c.track(id)
	out := subStructuredLyrics{Lang: "xxx", Synced: l.Sync != lyrics.SyncNone}
	if song != nil {
		out.DisplayArtist, out.DisplayTitle = song.Artist, song.Title
	}
	for _, line := range l.Lines {
		sl := subLyricsLine{Value: line.Text}
		if out.Synced {
			ms := int64(line.Start * 1000)
			sl.Start = &ms
		}
		out.Lines = append(out.Lines, sl)
	}
	return c.ok("lyricsList", list{[]subStructuredLyrics{out}})
}

// getLyrics (старый): по исполнителю и названию — первый трек поиска.
func subsonicLyrics(c *subCtx) error {
	type plain struct {
		Artist string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
		Title  string `xml:"title,attr,omitempty" json:"title,omitempty"`
		Value  string `xml:",chardata" json:"value,omitempty"`
	}
	artist, title := c.param("artist"), c.param("title")
	q := strings.TrimSpace(artist + " " + title)
	if q == "" {
		return c.ok("lyrics", plain{})
	}
	var found struct {
		Tracks []deezer.Track `json:"tracks"`
	}
	if err := c.call(c.s.handleSearch, "GET", "/?q="+url.QueryEscape(q), nil, nil, &found); err != nil || len(found.Tracks) == 0 {
		return c.ok("lyrics", plain{Artist: artist, Title: title})
	}
	t := found.Tracks[0]
	l, err := c.lyricsOf(t.ID)
	if err != nil || l == nil {
		return c.ok("lyrics", plain{Artist: t.Artist, Title: t.Title})
	}
	lines := make([]string, len(l.Lines))
	for i, line := range l.Lines {
		lines[i] = line.Text
	}
	return c.ok("lyrics", plain{Artist: t.Artist, Title: t.Title, Value: strings.Join(lines, "\n")})
}

// placeholder — серая заглушка, как на сайте: силуэт для артиста,
// пластинка для остального.
func placeholder(w http.ResponseWriter, artist bool) {
	img := placeholderDisc
	if artist {
		img = placeholderArtist
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(img())
}

var (
	placeholderArtist = sync.OnceValue(func() []byte {
		return drawPlaceholder(func(x, y float64) bool {
			head := (x-0.5)*(x-0.5)+(y-0.4)*(y-0.4) <= 0.15*0.15
			body := y <= 0.82 && ((x-0.5)/0.27)*((x-0.5)/0.27)+((y-0.84)/0.24)*((y-0.84)/0.24) <= 1
			return head || body
		})
	})
	placeholderDisc = sync.OnceValue(func() []byte {
		return drawPlaceholder(func(x, y float64) bool {
			d := (x-0.5)*(x-0.5) + (y-0.5)*(y-0.5)
			return d <= 0.3*0.3 && d >= 0.07*0.07
		})
	})
)

// drawPlaceholder — квадрат 600×600 цвета пустых обложек сайта, на нём
// фигура inside (координаты 0…1).
func drawPlaceholder(inside func(x, y float64) bool) []byte {
	const side = 600
	bg, fg := color.RGBA{0x28, 0x28, 0x28, 0xff}, color.RGBA{0x7f, 0x7f, 0x7f, 0xff}
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			c := bg
			if inside((float64(x)+0.5)/side, (float64(y)+0.5)/side) {
				c = fg
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
