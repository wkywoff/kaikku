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

	egresses   []*egress
	quotaDelay time.Duration

	tracks      ttlCache[Track]
	albums      ttlCache[Album]
	albumTracks ttlCache[[]Track]

	trackLoads     flight.Group[Track]
	albumLoads     flight.Group[Album]
	tracklistLoads flight.Group[[]Track]

	waits limitStats
}

const (
	apiRequestsPer5s = 40
	quotaRetries     = 2
)

func NewClient() *Client {
	return newClient("https://api.deezer.com", newRateLimiter(apiRequestsPer5s, 5*time.Second), 1500*time.Millisecond)
}

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
		tracks:      ttlCache[Track]{clone: cloneTrack, prefix: "track:"},
		albums:      ttlCache[Album]{clone: cloneAlbum, prefix: "album:"},
		albumTracks: ttlCache[[]Track]{clone: cloneTracks, prefix: "tracklist:"},
	}
}

func (c *Client) WithRateLimit(n int, per time.Duration) *Client {
	c.egresses[0].limiter = newRateLimiter(n, per)
	return c
}

type Track struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Artist       string      `json:"artist"`
	ArtistID     string      `json:"artist_id"`
	Album        string      `json:"album"`
	AlbumID      string      `json:"album_id"`
	TrackNo      int         `json:"track_no"`
	DiscNo       int         `json:"disc_no"`
	DurationMs   int         `json:"duration_ms"`
	CoverURL     string      `json:"cover_url"`
	Contributors []ArtistRef `json:"contributors,omitempty"`
	Explicit     bool        `json:"explicit"`
	ISRC         string      `json:"isrc,omitempty"`
	Hidden       bool        `json:"hidden,omitempty"`
}

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
	LibraryID  string `json:"library_id,omitempty"`
}

type Album struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	ArtistID      string      `json:"artist_id"`
	ArtistName    string      `json:"artist_name"`
	CoverURL      string      `json:"cover_url"`
	ReleaseDate   string      `json:"release_date"`
	RecordType    string      `json:"record_type"`
	NbTracks      int         `json:"nb_tracks,omitempty"`
	DurationSec   int         `json:"duration_sec,omitempty"`
	Label         string      `json:"label,omitempty"`
	Contributors  []ArtistRef `json:"contributors,omitempty"`
	MainArtistIDs []string    `json:"main_artist_ids,omitempty"`
	LibraryID     string      `json:"library_id,omitempty"`
	Explicit      bool        `json:"explicit,omitempty"`
	Hidden        bool        `json:"hidden,omitempty"`
	VersionOf     string      `json:"version_of,omitempty"`
}

type deezerArtistRef struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	PictureMedium string `json:"picture_medium"`
}

func imageURL(urls ...string) string {
	for _, u := range urls {
		_, rest, _ := strings.Cut(u, "://")
		if u != "" && !strings.Contains(rest, "//") {
			return u
		}
	}
	return ""
}

func coverURL(md5 string, urls ...string) string {
	if u := imageURL(urls...); u != "" || !isMD5(md5) {
		return u
	}
	return "https://cdn-images.dzcdn.net/images/cover/" + md5 + "/1000x1000-000000-80-0-0.jpg"
}

func isMD5(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func convertRefs(refs []deezerArtistRef) []ArtistRef {
	var out []ArtistRef
	for _, r := range refs {
		if r.Role != "" && r.Role != "Main" && r.Role != "Featured" {
			continue
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
	MD5Image string `json:"md5_image"`
}

type deezerTrack struct {
	ID            int64             `json:"id"`
	Title         string            `json:"title"`
	Duration      int               `json:"duration"`
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
	MD5Image     string            `json:"md5_image"`
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

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("deezer %s (code %d): %s", e.Type, e.Code, e.Message)
}

func (e *APIError) NotFound() bool { return e.Code == 800 }

func (e *APIError) overQuota() bool { return e.Code == 4 || e.Code == 700 }

func (c *Client) get(path string, out any) error { return c.fetch(path, out, false) }

func (c *Client) getObject(path string, out any) error { return c.fetch(path, out, true) }

func (c *Client) fetch(path string, out any, closedOK bool) error {
	var avoid *egress
	var proxyErr error
	for attempt := 0; ; attempt++ {
		e, waited := c.pick(avoid, false)
		if e == nil && closedOK {
			e, waited = c.pick(avoid, true)
		}
		c.waits.waited(waited)
		if e == nil {
			if proxyErr != nil {
				return proxyErr
			}
			return c.unavailable()
		}
		err := c.getOnce(e, path, out)
		var apiErr *APIError
		isAPIErr := errors.As(err, &apiErr)
		if e.proxy {
			e.done(err, c.pauseLimit(e))
			if err != nil && !isAPIErr && avoid == nil {
				avoid, proxyErr = e, err
				continue
			}
		}
		if isAPIErr && apiErr.overQuota() {
			c.waits.overQuota()
			if e.proxy && avoid == nil && c.openOther(e) {
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

func getList[T any](c *Client, path string) ([]T, error) {
	return fetchList[T](c, path, false)
}

func fetchList[T any](c *Client, path string, closedOK bool) ([]T, error) {
	var resp deezerListResponse[T]
	if err := c.fetch(path, &resp, closedOK); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 && resp.Total > 0 {
		return nil, fmt.Errorf("deezer returned no rows for total=%d (likely an IP/region restriction)", resp.Total)
	}
	return resp.Data, nil
}

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

func (c *Client) GetTrack(id string) (*Track, error) {
	if t, ok := c.tracks.get(id); ok {
		return &t, nil
	}
	track, err := c.trackLoads.Do(id, func() (Track, error) {
		var t deezerTrack
		if err := c.getObject("/track/"+url.PathEscape(id), &t); err != nil {
			return Track{}, fmt.Errorf("get track: %w", err)
		}
		track := convertTrack(t)
		ttl := trackCacheTTL
		if track.CoverURL == "" {
			ttl = freshCacheTTL
		}
		c.tracks.putFor(id, track, ttl)
		return track, nil
	})
	if err != nil {
		return nil, err
	}
	track = cloneTrack(track)
	return &track, nil
}

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

func (c *Client) GetArtist(id string) (*Artist, error) {
	var a deezerArtist
	if err := c.getObject("/artist/"+url.PathEscape(id), &a); err != nil {
		return nil, fmt.Errorf("get artist: %w", err)
	}
	artist := convertArtist(a)
	return &artist, nil
}

const artistAlbumsPage = 100

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

func (c *Client) GetAlbum(id string) (*Album, error) {
	if a, ok := c.albums.get(id); ok {
		return &a, nil
	}
	album, err := c.albumLoads.Do(id, func() (Album, error) {
		var a deezerAlbum
		if err := c.getObject("/album/"+url.PathEscape(id), &a); err != nil {
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

func (c *Client) HasTrack(id string) bool { return c.tracks.has(id) }

func (c *Client) GetAlbumTracks(album *Album, limit int) ([]Track, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	key := album.ID + ":" + strconv.Itoa(limit)
	tracks, ok := c.albumTracks.get(key)
	if !ok {
		loaded, err := c.tracklistLoads.Do(key, func() ([]Track, error) {
			path := fmt.Sprintf("/album/%s/tracks?limit=%d", url.PathEscape(album.ID), limit)
			data, err := fetchList[deezerTrack](c, path, true)
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
	for i := range tracks {
		tracks[i].AlbumID = album.ID
		tracks[i].Album = album.Title
		tracks[i].CoverURL = album.CoverURL
	}
	return tracks, nil
}

func convertTrack(t deezerTrack) Track {
	coverURL := coverURL(t.Album.MD5Image, t.Album.CoverXL, t.Album.CoverBig)
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
	cover := coverURL(a.MD5Image, a.CoverXL, a.CoverBig)
	artistID := artistRef.ID
	artistName := artistRef.Name
	if artistID == 0 {
		artistID = a.Artist.ID
		artistName = a.Artist.Name
	}
	var main []string
	for _, r := range a.Contributors {
		if r.Role == "Main" {
			main = append(main, strconv.FormatInt(r.ID, 10))
		}
	}
	return Album{
		ID:            strconv.FormatInt(a.ID, 10),
		Title:         a.Title,
		ArtistID:      strconv.FormatInt(artistID, 10),
		ArtistName:    artistName,
		CoverURL:      cover,
		ReleaseDate:   a.ReleaseDate,
		RecordType:    a.RecordType,
		NbTracks:      a.NbTracks,
		DurationSec:   a.Duration,
		Label:         a.Label,
		Explicit:      a.Explicit,
		Contributors:  convertRefs(a.Contributors),
		MainArtistIDs: main,
	}
}

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
