// Package deezer wraps the parts of Deezer's public API this project needs:
// track/artist search, artist discography, and album tracklists. All of
// these are unauthenticated GET requests (no API key, no OAuth).
//
// Deezer gives metadata and 30-second previews only, never full audio — the
// audio comes from YouTube via yt-dlp, matched by artist/title/duration
// (internal/ytdlp.FindMatches).
package deezer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kaikku/internal/flight"
)

type Client struct {
	apiURL string

	// egresses — выходы к Deezer (egress.go): первый — напрямую, дальше —
	// через прокси с другим IP.
	egresses   []*egress
	quotaDelay time.Duration // пауза перед повтором, если Deezer ответил «квота»

	tracks      ttlCache[Track]
	albums      ttlCache[Album]
	albumTracks ttlCache[[]Track] // треклисты без полей альбома (их ставит GetAlbumTracks)

	// Промахи кеша с одним ключом в одно время — одним запросом к Deezer.
	trackLoads     flight.Group[Track]
	albumLoads     flight.Group[Album]
	tracklistLoads flight.Group[[]Track]

	waits limitStats // сколько запросов ждали лимита (limit.go)
}

// Доля лимита Deezer (50 запросов / 5 с на IP) для API; воркеру остаётся
// запас (cmd/worker задаёт себе меньше через WithRateLimit).
const (
	apiRequestsPer5s = 40
	quotaRetries     = 2
)

func NewClient() *Client {
	return newClient("https://api.deezer.com", newRateLimiter(apiRequestsPer5s, 5*time.Second), 1500*time.Millisecond)
}

// NewClientWithBaseURL lets tests point the client at a mock server (no
// rate limit, a short pause before retrying on "quota exceeded").
func NewClientWithBaseURL(apiURL string) *Client {
	return newClient(apiURL, nil, 10*time.Millisecond)
}

func newClient(apiURL string, limiter *rateLimiter, quotaDelay time.Duration) *Client {
	return &Client{
		apiURL: apiURL,
		egresses: []*egress{{
			name: "direct", http: &http.Client{Timeout: 10 * time.Second}, limiter: limiter,
		}},
		quotaDelay:  quotaDelay,
		tracks:      ttlCache[Track]{ttl: trackCacheTTL, clone: cloneTrack, prefix: "track:"},
		albums:      ttlCache[Album]{ttl: albumCacheTTL, clone: cloneAlbum, prefix: "album:"},
		albumTracks: ttlCache[[]Track]{ttl: tracklistCacheTTL, clone: cloneTracks, prefix: "tracklist:"},
	}
}

// WithRateLimit sets this client's share of Deezer's request limit: at most
// n requests in any window of per.
func (c *Client) WithRateLimit(n int, per time.Duration) *Client {
	c.egresses[0].limiter = newRateLimiter(n, per)
	return c
}

// Track is the flattened shape we actually use, out of Deezer's more deeply
// nested response.
type Track struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	ArtistID   string `json:"artist_id"`
	Album      string `json:"album"`
	AlbumID    string `json:"album_id"`
	TrackNo    int    `json:"track_no"`
	DiscNo     int    `json:"disc_no"`
	DurationMs int    `json:"duration_ms"`
	CoverURL   string `json:"cover_url"`
	// Все исполнители трека (основной + фиты), если Deezer их отдал —
	// /track и /artist/{id}/top отдают, треклист альбома и поиск — нет
	// (для них см. Client.EnrichContributors).
	Contributors []ArtistRef `json:"contributors,omitempty"`
	Explicit     bool        `json:"explicit"`
	// ISRC — код записи: у одной записи на разных релизах (сингл, альбом,
	// делюкс) он один, хотя Deezer ID разные.
	ISRC string `json:"isrc,omitempty"`
	// Hidden — релиз трека скрыт админом (ставит API, не Deezer).
	Hidden bool `json:"hidden,omitempty"`
}

// ArtistRef — исполнитель в составе трека или альбома.
type ArtistRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url,omitempty"`
}

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url"`
	Fans       int    `json:"fans,omitempty"`
	// LibraryID — ID артиста в нашей медиатеке, если он там есть (ставит
	// API при наложении ручных правок; Deezer его не отдаёт).
	LibraryID string `json:"library_id,omitempty"`
}

type Album struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ArtistID    string `json:"artist_id"`
	ArtistName  string `json:"artist_name"`
	CoverURL    string `json:"cover_url"`
	ReleaseDate string `json:"release_date"`
	// RecordType: "album", "ep", "single" или "compile" (сборник).
	RecordType  string `json:"record_type"`
	NbTracks    int    `json:"nb_tracks,omitempty"`
	DurationSec int    `json:"duration_sec,omitempty"`
	Label       string `json:"label,omitempty"`
	// Главные исполнители альбома (с фото) — для шапки страницы альбома.
	Contributors []ArtistRef `json:"contributors,omitempty"`
	// LibraryID — ID альбома в нашей медиатеке, если он там есть.
	LibraryID string `json:"library_id,omitempty"`
	// Explicit — релиз с ненормативной лексикой (у clean-версии false).
	Explicit bool `json:"explicit,omitempty"`
	// Hidden — релиз скрыт админом (ставит API, не Deezer).
	Hidden bool `json:"hidden,omitempty"`
	// VersionOf — ID основного релиза (Deezer или свой k…), если этот
	// привязан к нему как версия (делюкс, clean); ставит API.
	VersionOf string `json:"version_of,omitempty"`
}

// --- raw Deezer response shapes (unexported, converted on the way out) ---

type deezerArtistRef struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	PictureMedium string `json:"picture_medium"`
}

// imageURL returns the first real image among urls (largest first). Without
// a picture Deezer still sends a link, with an empty hash
// (".../images/artist//1000x1000-....jpg"), which only its own CDN answers —
// with a grey placeholder; we treat it as "no image" and draw our own.
func imageURL(urls ...string) string {
	for _, u := range urls {
		_, rest, _ := strings.Cut(u, "://")
		if u != "" && !strings.Contains(rest, "//") {
			return u
		}
	}
	return ""
}

func convertRefs(refs []deezerArtistRef) []ArtistRef {
	var out []ArtistRef
	for _, r := range refs {
		if r.Role != "" && r.Role != "Main" && r.Role != "Featured" {
			continue // композиторы и т.п. — не исполнители
		}
		out = append(out, ArtistRef{ID: strconv.FormatInt(r.ID, 10), Name: r.Name, PictureURL: imageURL(r.PictureMedium)})
	}
	return out
}

type deezerAlbumRef struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	CoverXL  string `json:"cover_xl"`
	CoverBig string `json:"cover_big"`
}

type deezerTrack struct {
	ID            int64             `json:"id"`
	Title         string            `json:"title"`
	Duration      int               `json:"duration"` // секунды
	TrackPosition int               `json:"track_position"`
	DiskNumber    int               `json:"disk_number"`
	Explicit      bool              `json:"explicit_lyrics"`
	ISRC          string            `json:"isrc"`
	Artist        deezerArtistRef   `json:"artist"`
	Album         deezerAlbumRef    `json:"album"`
	Contributors  []deezerArtistRef `json:"contributors"`
}

type deezerArtist struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PictureXL  string `json:"picture_xl"`
	PictureBig string `json:"picture_big"`
	NbFan      int    `json:"nb_fan"`
}

type deezerAlbum struct {
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	CoverXL      string            `json:"cover_xl"`
	CoverBig     string            `json:"cover_big"`
	ReleaseDate  string            `json:"release_date"`
	RecordType   string            `json:"record_type"`
	NbTracks     int               `json:"nb_tracks"`
	Duration     int               `json:"duration"`
	Label        string            `json:"label"`
	Explicit     bool              `json:"explicit_lyrics"`
	Artist       deezerArtistRef   `json:"artist"`
	Contributors []deezerArtistRef `json:"contributors"`
}

type deezerListResponse[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

// APIError is an error Deezer reports in the response body. Deezer answers
// HTTP 200 even for errors — e.g. {"error":{"type":"DataException",
// "message":"no data","code":800}} for an unknown ID, code 4 for the
// 50 requests / 5 s quota.
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("deezer %s (code %d): %s", e.Type, e.Code, e.Message)
}

// NotFound reports whether Deezer said the requested object doesn't exist.
func (e *APIError) NotFound() bool { return e.Code == 800 }

// overQuota: Deezer refused because of its request limit (code 4) or was
// briefly overloaded (700, "service busy") — worth retrying after a pause.
func (e *APIError) overQuota() bool { return e.Code == 4 || e.Code == 700 }

// get fetches path into out, keeping to the rate limit and retrying a few
// times when Deezer says the quota is exceeded. Если через прокси Deezer не
// ответил (соединение, таймаут, не тот ответ) или ответил «квота», запрос
// сразу повторяется напрямую.
func (c *Client) get(path string, out any) error {
	var avoid *egress
	for attempt := 0; ; attempt++ {
		e, waited := c.pick(avoid)
		c.waits.waited(waited)
		err := c.getOnce(e, path, out)
		var apiErr *APIError
		isAPIErr := errors.As(err, &apiErr)
		if e.proxy {
			e.done(err)
			if err != nil && !isAPIErr && avoid == nil {
				avoid = e
				continue
			}
		}
		if isAPIErr && apiErr.overQuota() {
			c.waits.overQuota()
			if e.proxy && avoid == nil {
				avoid = e
				continue
			}
			if attempt < quotaRetries {
				time.Sleep(c.quotaDelay * time.Duration(attempt+1))
				continue
			}
		}
		return err
	}
}

func (c *Client) getOnce(e *egress, path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.apiURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return &errTransport{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	var envelope struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// getList is get for list endpoints. Deezer has been observed returning
// {"data":[],"total":236} from some networks — a non-empty result set with
// the rows withheld. Passing that through as "nothing found" hides the real
// problem, so it's an error.
func getList[T any](c *Client, path string) ([]T, error) {
	var resp deezerListResponse[T]
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 && resp.Total > 0 {
		return nil, fmt.Errorf("deezer returned no rows for total=%d (likely an IP/region restriction)", resp.Total)
	}
	return resp.Data, nil
}

// Search queries the public catalog for tracks matching q.
func (c *Client) Search(q string, limit int) ([]Track, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	path := "/search?" + url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}.Encode()
	data, err := getList[deezerTrack](c, path)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	tracks := make([]Track, 0, len(data))
	for _, t := range data {
		tracks = append(tracks, convertTrack(t))
	}
	return tracks, nil
}

// GetTrack authoritatively resolves one track by Deezer ID — used at ingest
// time instead of trusting whatever metadata a client might send (the
// client only ever supplies the ID, see handlers_ingest.go).
//
// Cached (cache.go): the same track is asked for by the player, the play
// history, likes and the contributor lookup.
func (c *Client) GetTrack(id string) (*Track, error) {
	if t, ok := c.tracks.get(id); ok {
		return &t, nil
	}
	track, err := c.trackLoads.Do(id, func() (Track, error) {
		var t deezerTrack
		if err := c.get("/track/"+url.PathEscape(id), &t); err != nil {
			return Track{}, fmt.Errorf("get track: %w", err)
		}
		track := convertTrack(t)
		c.tracks.put(id, track)
		return track, nil
	})
	if err != nil {
		return nil, err
	}
	track = cloneTrack(track)
	return &track, nil
}

// SearchArtists queries the public catalog for artists matching q.
func (c *Client) SearchArtists(q string, limit int) ([]Artist, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	path := "/search/artist?" + url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}.Encode()
	data, err := getList[deezerArtist](c, path)
	if err != nil {
		return nil, fmt.Errorf("search artists: %w", err)
	}
	artists := make([]Artist, 0, len(data))
	for _, a := range data {
		artists = append(artists, convertArtist(a))
	}
	return artists, nil
}

// SearchAlbums queries the public catalog for albums matching q.
func (c *Client) SearchAlbums(q string, limit int) ([]Album, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	path := "/search/album?" + url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}.Encode()
	data, err := getList[deezerAlbum](c, path)
	if err != nil {
		return nil, fmt.Errorf("search albums: %w", err)
	}
	albums := make([]Album, 0, len(data))
	for _, a := range data {
		albums = append(albums, convertAlbum(a, a.Artist))
	}
	return albums, nil
}

// GetArtistTop returns an artist's most popular tracks.
func (c *Client) GetArtistTop(id string, limit int) ([]Track, error) {
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	path := fmt.Sprintf("/artist/%s/top?limit=%d", url.PathEscape(id), limit)
	data, err := getList[deezerTrack](c, path)
	if err != nil {
		return nil, fmt.Errorf("get artist top: %w", err)
	}
	tracks := make([]Track, 0, len(data))
	for _, t := range data {
		tracks = append(tracks, convertTrack(t))
	}
	return tracks, nil
}

// GetArtist resolves one artist by Deezer ID.
func (c *Client) GetArtist(id string) (*Artist, error) {
	var a deezerArtist
	if err := c.get("/artist/"+url.PathEscape(id), &a); err != nil {
		return nil, fmt.Errorf("get artist: %w", err)
	}
	artist := convertArtist(a)
	return &artist, nil
}

// artistAlbumsPage is the largest page Deezer serves for /artist/{id}/albums.
const artistAlbumsPage = 100

// GetArtistAlbums returns an artist's discography, up to max releases. Deezer
// pages it 100 at a time (newest first), so prolific artists' older releases
// live on later pages — we walk them with index= until the list runs out.
func (c *Client) GetArtistAlbums(id string, max int) ([]Album, error) {
	if max <= 0 {
		max = artistAlbumsPage
	}
	var albums []Album
	for len(albums) < max {
		path := fmt.Sprintf("/artist/%s/albums?limit=%d&index=%d", url.PathEscape(id), artistAlbumsPage, len(albums))
		var resp deezerListResponse[deezerAlbum]
		if err := c.get(path, &resp); err != nil {
			return nil, fmt.Errorf("get artist albums: %w", err)
		}
		if len(resp.Data) == 0 && resp.Total > 0 && len(albums) == 0 {
			return nil, fmt.Errorf("get artist albums: deezer returned no rows for total=%d (likely an IP/region restriction)", resp.Total)
		}
		for _, a := range resp.Data {
			album := convertAlbum(a, deezerArtistRef{})
			// Deezer's /artist/{id}/albums entries typically omit the "implied"
			// artist ref (redundant since you already asked for this artist's
			// albums) — the id param here is authoritative regardless.
			album.ArtistID = id
			albums = append(albums, album)
		}
		if len(resp.Data) < artistAlbumsPage || (resp.Total > 0 && len(albums) >= resp.Total) {
			break
		}
	}
	if len(albums) > max {
		albums = albums[:max]
	}
	return albums, nil
}

// GetAlbum resolves one album's own metadata (without its tracklist — use
// GetAlbumTracks for that, since Deezer's /album/{id} response nests tracks
// under a differently-shaped "data" wrapper we don't need to parse twice).
func (c *Client) GetAlbum(id string) (*Album, error) {
	if a, ok := c.albums.get(id); ok {
		return &a, nil
	}
	album, err := c.albumLoads.Do(id, func() (Album, error) {
		var a deezerAlbum
		if err := c.get("/album/"+url.PathEscape(id), &a); err != nil {
			return Album{}, fmt.Errorf("get album: %w", err)
		}
		album := convertAlbum(a, a.Artist)
		ttl := albumCacheTTL
		if freshRelease(album.ReleaseDate) {
			ttl = freshCacheTTL
		}
		c.albums.putFor(id, album, ttl)
		return album, nil
	})
	if err != nil {
		return nil, err
	}
	album = cloneAlbum(album)
	return &album, nil
}

// AlbumRequests — сколько запросов к Deezer будут стоить альбом id и его
// треклист из limit треков (0 — оба в кеше).
func (c *Client) AlbumRequests(id string, limit int) int {
	n := 0
	if !c.albums.has(id) {
		n++
	}
	if !c.albumTracks.has(id + ":" + strconv.Itoa(limit)) {
		n++
	}
	return n
}

// HasTrack — трек уже в кеше (со всеми исполнителями).
func (c *Client) HasTrack(id string) bool { return c.tracks.has(id) }

// GetAlbumTracks returns an album's full, correctly-ordered tracklist.
// Deezer's /album/{id}/tracks entries carry no "album" object at all (only
// the parent's id is implied by the URL) — so the album's own fields
// (title, cover) are filled from album, which callers get via GetAlbum.
func (c *Client) GetAlbumTracks(album *Album, limit int) ([]Track, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	key := album.ID + ":" + strconv.Itoa(limit)
	tracks, ok := c.albumTracks.get(key)
	if !ok {
		loaded, err := c.tracklistLoads.Do(key, func() ([]Track, error) {
			path := fmt.Sprintf("/album/%s/tracks?limit=%d", url.PathEscape(album.ID), limit)
			data, err := getList[deezerTrack](c, path)
			if err != nil {
				return nil, fmt.Errorf("get album tracks: %w", err)
			}
			tracks := make([]Track, 0, len(data))
			for _, t := range data {
				tracks = append(tracks, convertTrack(t))
			}
			ttl := tracklistCacheTTL
			if freshRelease(album.ReleaseDate) || len(tracks) < min(album.NbTracks, limit) {
				ttl = freshCacheTTL
			}
			c.albumTracks.putFor(key, tracks, ttl)
			return tracks, nil
		})
		if err != nil {
			return nil, err
		}
		tracks = cloneTracks(loaded)
	}
	// Поля альбома — из переданного: он может быть с правками админа, а кеш —
	// ответ самого Deezer.
	for i := range tracks {
		tracks[i].AlbumID = album.ID
		tracks[i].Album = album.Title
		tracks[i].CoverURL = album.CoverURL
	}
	return tracks, nil
}

func convertTrack(t deezerTrack) Track {
	coverURL := imageURL(t.Album.CoverXL, t.Album.CoverBig)
	return Track{
		ID:           strconv.FormatInt(t.ID, 10),
		Title:        t.Title,
		Artist:       t.Artist.Name,
		ArtistID:     strconv.FormatInt(t.Artist.ID, 10),
		Album:        t.Album.Title,
		AlbumID:      strconv.FormatInt(t.Album.ID, 10),
		TrackNo:      t.TrackPosition,
		DiscNo:       t.DiskNumber,
		DurationMs:   t.Duration * 1000,
		CoverURL:     coverURL,
		Contributors: convertRefs(t.Contributors),
		Explicit:     t.Explicit,
		ISRC:         t.ISRC,
	}
}

func convertArtist(a deezerArtist) Artist {
	pic := imageURL(a.PictureXL, a.PictureBig)
	return Artist{
		ID:         strconv.FormatInt(a.ID, 10),
		Name:       a.Name,
		PictureURL: pic,
		Fans:       a.NbFan,
	}
}

func convertAlbum(a deezerAlbum, artistRef deezerArtistRef) Album {
	cover := imageURL(a.CoverXL, a.CoverBig)
	artistID := artistRef.ID
	artistName := artistRef.Name
	if artistID == 0 {
		artistID = a.Artist.ID
		artistName = a.Artist.Name
	}
	return Album{
		ID:           strconv.FormatInt(a.ID, 10),
		Title:        a.Title,
		ArtistID:     strconv.FormatInt(artistID, 10),
		ArtistName:   artistName,
		CoverURL:     cover,
		ReleaseDate:  a.ReleaseDate,
		RecordType:   a.RecordType,
		NbTracks:     a.NbTracks,
		DurationSec:  a.Duration,
		Label:        a.Label,
		Explicit:     a.Explicit,
		Contributors: convertRefs(a.Contributors),
	}
}

// GetArtistRelated returns artists Deezer considers similar ("Fans also
// like") — at most 20, Deezer's own cap.
func (c *Client) GetArtistRelated(id string, limit int) ([]Artist, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	path := fmt.Sprintf("/artist/%s/related?limit=%d", url.PathEscape(id), limit)
	data, err := getList[deezerArtist](c, path)
	if err != nil {
		return nil, fmt.Errorf("get related artists: %w", err)
	}
	artists := make([]Artist, 0, len(data))
	for _, a := range data {
		artists = append(artists, convertArtist(a))
	}
	return artists, nil
}

// GetArtistRadio returns Deezer's radio for an artist: ~25 tracks by them
// and similar artists, a fresh random pick on every call.
func (c *Client) GetArtistRadio(id string) ([]Track, error) {
	data, err := getList[deezerTrack](c, fmt.Sprintf("/artist/%s/radio?limit=50", url.PathEscape(id)))
	if err != nil {
		return nil, fmt.Errorf("get artist radio: %w", err)
	}
	tracks := make([]Track, 0, len(data))
	for _, t := range data {
		tracks = append(tracks, convertTrack(t))
	}
	return tracks, nil
}
