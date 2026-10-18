package api

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"kaikku/internal/deezer"
)

// Медиатека приложения — то же, что «Медиатека» на сайте: любимые треки,
// сохранённые альбомы и артисты (подписки и исполнители сохранённого).
// Остальной каталог Deezer — через поиск и страницы артистов.

func init() {
	subsonicRoute("getArtists", subsonicGetArtists)
	subsonicRoute("getIndexes", subsonicGetIndexes)
	subsonicRoute("getArtist", subsonicGetArtist)
	subsonicRoute("getAlbum", subsonicGetAlbum)
	subsonicRoute("getSong", subsonicGetSong)
	subsonicRoute("getMusicDirectory", subsonicGetMusicDirectory)
	subsonicRoute("getAlbumList2", func(c *subCtx) error { return subsonicAlbumList(c, true) })
	subsonicRoute("getAlbumList", func(c *subCtx) error { return subsonicAlbumList(c, false) })
	subsonicRoute("getStarred2", func(c *subCtx) error { return subsonicStarred(c, true) })
	subsonicRoute("getStarred", func(c *subCtx) error { return subsonicStarred(c, false) })
	subsonicRoute("star", func(c *subCtx) error { return subsonicStar(c, true) })
	subsonicRoute("unstar", func(c *subCtx) error { return subsonicStar(c, false) })
	subsonicRoute("search3", func(c *subCtx) error { return subsonicSearch(c, true) })
	subsonicRoute("search2", func(c *subCtx) error { return subsonicSearch(c, false) })
}

// intParam — число из параметра name, def без него, не больше max.
func (c *subCtx) intParam(name string, def, max int) int {
	n, err := strconv.Atoi(c.param(name))
	if err != nil || n < 0 {
		return def
	}
	return min(n, max)
}

func page[T any](items []T, offset, size int) []T {
	if offset >= len(items) {
		return nil
	}
	return items[offset:min(len(items), offset+size)]
}

// --- медиатека ---

type subLibrary struct {
	liked    []deezer.Track
	albums   []deezer.Album
	followed []deezer.Artist
	artists  []deezer.Artist // подписки и исполнители сохранённого
	counts   map[string]int  // артист → сколько его альбомов в медиатеке
}

func (c *subCtx) library() (subLibrary, error) {
	var liked struct {
		Tracks []collectionTrack `json:"tracks"`
	}
	var saved struct {
		Albums []deezer.Album `json:"albums"`
	}
	var followed struct {
		Artists []deezer.Artist `json:"artists"`
	}
	if err := c.call(c.s.handleLikedTracks, "GET", "/", nil, nil, &liked); err != nil {
		return subLibrary{}, err
	}
	if err := c.call(c.s.handleSavedAlbums, "GET", "/", nil, nil, &saved); err != nil {
		return subLibrary{}, err
	}
	if err := c.call(c.s.handleFollowedArtists, "GET", "/", nil, nil, &followed); err != nil {
		return subLibrary{}, err
	}
	lib := subLibrary{albums: saved.Albums, followed: followed.Artists, counts: map[string]int{}}
	lib.artists = slices.Clone(lib.followed)
	for _, t := range liked.Tracks {
		lib.liked = append(lib.liked, t.Track)
	}

	// Артисты: подписки, потом исполнители сохранённых альбомов и любимых треков.
	seen := map[string]bool{}
	for _, a := range lib.artists {
		seen[a.ID] = true
	}
	add := func(id, name string) {
		if id != "" && name != "" && !seen[id] {
			seen[id] = true
			lib.artists = append(lib.artists, deezer.Artist{ID: id, Name: name})
		}
	}
	albumsOf := map[string]map[string]bool{}
	count := func(artist, album string) {
		if albumsOf[artist] == nil {
			albumsOf[artist] = map[string]bool{}
		}
		albumsOf[artist][album] = true
	}
	for _, a := range lib.albums {
		if !a.Hidden {
			add(a.ArtistID, a.ArtistName)
			count(a.ArtistID, a.ID)
		}
	}
	for _, t := range lib.liked {
		if !t.Hidden {
			add(t.ArtistID, t.Artist)
			count(t.ArtistID, t.AlbumID)
		}
	}
	for artist, albums := range albumsOf {
		lib.counts[artist] = len(albums)
	}
	return lib, nil
}

type subIndex[T any] struct {
	Name    string `xml:"name,attr" json:"name"`
	Artists []T    `xml:"artist" json:"artist,omitempty"`
}

// indexLetter — буква оглавления: первая буква имени, остальное — «#».
func indexLetter(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
		return "#"
	}
	return "#"
}

func indexArtists[T any](artists []subArtistID3, conv func(subArtistID3) T) []subIndex[T] {
	sort.SliceStable(artists, func(i, j int) bool {
		return strings.ToLower(artists[i].Name) < strings.ToLower(artists[j].Name)
	})
	var out []subIndex[T]
	for _, a := range artists {
		letter := indexLetter(a.Name)
		if len(out) == 0 || out[len(out)-1].Name != letter {
			out = append(out, subIndex[T]{Name: letter})
		}
		out[len(out)-1].Artists = append(out[len(out)-1].Artists, conv(a))
	}
	// «#» — в конце, как у остальных серверов.
	slices.SortStableFunc(out, func(a, b subIndex[T]) int {
		switch {
		case a.Name == b.Name:
			return 0
		case a.Name == "#":
			return 1
		case b.Name == "#":
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func subsonicGetArtists(c *subCtx) error {
	lib, err := c.library()
	if err != nil {
		return err
	}
	return c.ok("artists", struct {
		IgnoredArticles string                   `xml:"ignoredArticles,attr" json:"ignoredArticles"`
		Index           []subIndex[subArtistID3] `xml:"index" json:"index,omitempty"`
	}{"", indexArtists(c.artists(lib.artists, lib.counts), func(a subArtistID3) subArtistID3 { return a })})
}

func subsonicGetIndexes(c *subCtx) error {
	lib, err := c.library()
	if err != nil {
		return err
	}
	return c.ok("indexes", struct {
		LastModified    int64                      `xml:"lastModified,attr" json:"lastModified"`
		IgnoredArticles string                     `xml:"ignoredArticles,attr" json:"ignoredArticles"`
		Index           []subIndex[subArtistPlain] `xml:"index" json:"index,omitempty"`
	}{time.Now().UnixMilli(), "", indexArtists(c.artists(lib.artists, lib.counts), func(a subArtistID3) subArtistPlain {
		return subArtistPlain{ID: a.ID, Name: a.Name, Starred: a.Starred}
	})})
}

// --- артист, альбом, трек ---

type artistPage struct {
	Artist deezer.Artist  `json:"artist,omitempty"`
	Albums []deezer.Album `json:"albums"`
	Top    []deezer.Track `json:"top"`
}

func (c *subCtx) artistPage(id string) (artistPage, error) {
	var p artistPage
	err := c.call(c.s.handleDiscoverArtist, "GET", "/", map[string]string{"id": id}, nil, &p)
	return p, err
}

func subsonicGetArtist(c *subCtx) error {
	id, err := c.catalogParam("id", subArtist)
	if err != nil {
		return err
	}
	p, err := c.artistPage(id)
	if err != nil {
		return err
	}
	albums := c.albums(p.Albums)
	a := c.artists([]deezer.Artist{p.Artist}, map[string]int{p.Artist.ID: len(albums)})[0]
	a.Albums = albums
	return c.ok("artist", a)
}

type albumPage struct {
	Album  deezer.Album   `json:"album,omitempty"`
	Tracks []deezer.Track `json:"tracks"`
}

func (c *subCtx) albumPage(id string) (albumPage, error) {
	var p albumPage
	cost := 0
	if !isLocalID(id) {
		cost = c.s.Deezer.AlbumRequests(id, 200) // как albumTracks
	}
	release, err := c.deezerShare(cost)
	if err != nil {
		return p, err
	}
	defer release()
	err = c.call(c.s.handleDiscoverAlbum, "GET", "/", map[string]string{"id": id}, nil, &p)
	if err == nil && p.Album.Hidden {
		err = subFail(subErrNotFound, "album not found")
	}
	return p, err
}

func (c *subCtx) albumWithSongs(p albumPage) subAlbumID3 {
	a := c.album(p.Album, c.starred("saved_albums", []string{p.Album.ID}))
	a.Songs = c.songs(p.Tracks)
	a.SongCount = len(a.Songs)
	a.Duration = 0
	for i := range a.Songs {
		a.Songs[i].Year = a.Year
		a.Duration += a.Songs[i].Duration
	}
	return a
}

func subsonicGetAlbum(c *subCtx) error {
	id, err := c.catalogParam("id", subAlbum)
	if err != nil {
		return err
	}
	p, err := c.albumPage(id)
	if err != nil {
		return err
	}
	return c.ok("album", c.albumWithSongs(p))
}

func (c *subCtx) track(id string) (*subSong, error) {
	var t deezer.Track
	if err := c.call(c.s.handleDiscoverTrack, "GET", "/", map[string]string{"id": id}, nil, &t); err != nil {
		return nil, err
	}
	songs := c.songs([]deezer.Track{t})
	if len(songs) == 0 {
		return nil, subFail(subErrNotFound, "song not found")
	}
	return &songs[0], nil
}

func subsonicGetSong(c *subCtx) error {
	id, err := c.catalogParam("id", subTrack)
	if err != nil {
		return err
	}
	s, err := c.track(id)
	if err != nil {
		return err
	}
	return c.ok("song", s)
}

// getMusicDirectory — «папки»: артист → его альбомы, альбом → треки.
func subsonicGetMusicDirectory(c *subCtx) error {
	type directory struct {
		ID       string    `xml:"id,attr" json:"id"`
		Parent   string    `xml:"parent,attr,omitempty" json:"parent,omitempty"`
		Name     string    `xml:"name,attr" json:"name"`
		Starred  string    `xml:"starred,attr,omitempty" json:"starred,omitempty"`
		Children []subSong `xml:"child" json:"child,omitempty"`
	}
	id := c.param("id")
	switch kind, raw := parseSubID(id); {
	case kind == subArtist && validCatalogID(raw):
		p, err := c.artistPage(raw)
		if err != nil {
			return err
		}
		a := c.artists([]deezer.Artist{p.Artist}, nil)[0]
		return c.ok("directory", directory{ID: a.ID, Name: a.Name, Starred: a.Starred, Children: c.albumDirs(p.Albums)})
	case kind == subAlbum && validCatalogID(raw):
		p, err := c.albumPage(raw)
		if err != nil {
			return err
		}
		a := c.albumWithSongs(p)
		return c.ok("directory", directory{ID: a.ID, Parent: a.ArtistID, Name: a.Name, Starred: a.Starred, Children: a.Songs})
	}
	return subFail(subErrNotFound, "no such directory: "+id)
}

// --- списки альбомов ---

func (c *subCtx) albumListOf(typ string) ([]deezer.Album, error) {
	switch typ {
	case "newest":
		var out struct {
			Albums []deezer.Album `json:"albums"`
		}
		err := c.call(c.s.handleNewReleases, "GET", "/", nil, nil, &out)
		return out.Albums, err
	case "recent":
		var out struct {
			Items []recentItem `json:"items"`
		}
		if err := c.call(c.s.handleRecent, "GET", "/", nil, nil, &out); err != nil {
			return nil, err
		}
		var albums []deezer.Album
		for _, it := range out.Items {
			if it.Album != nil {
				albums = append(albums, *it.Album)
			}
		}
		return albums, nil
	case "frequent", "highest":
		return c.frequentAlbums()
	case "random", "starred", "alphabeticalByName", "alphabeticalByArtist", "byYear":
		var saved struct {
			Albums []deezer.Album `json:"albums"`
		}
		if err := c.call(c.s.handleSavedAlbums, "GET", "/", nil, nil, &saved); err != nil {
			return nil, err
		}
		albums := saved.Albums
		lower := strings.ToLower
		switch typ {
		case "random":
			rand.Shuffle(len(albums), func(i, j int) { albums[i], albums[j] = albums[j], albums[i] })
		case "alphabeticalByName":
			sort.SliceStable(albums, func(i, j int) bool { return lower(albums[i].Title) < lower(albums[j].Title) })
		case "alphabeticalByArtist":
			sort.SliceStable(albums, func(i, j int) bool {
				if a, b := lower(albums[i].ArtistName), lower(albums[j].ArtistName); a != b {
					return a < b
				}
				return lower(albums[i].Title) < lower(albums[j].Title)
			})
		case "byYear":
			from, to := c.intParam("fromYear", 0, 9999), c.intParam("toYear", 9999, 9999)
			lo, hi := min(from, to), max(from, to)
			albums = slices.DeleteFunc(albums, func(a deezer.Album) bool {
				y := releaseYear(a.ReleaseDate)
				return y < lo || y > hi
			})
			sort.SliceStable(albums, func(i, j int) bool {
				if from > to {
					return albums[i].ReleaseDate > albums[j].ReleaseDate
				}
				return albums[i].ReleaseDate < albums[j].ReleaseDate
			})
		}
		return albums, nil
	}
	if fn := subsonicAlbumLists[typ]; fn != nil {
		return fn(c)
	}
	return nil, subFail(subErrGeneric, "unknown list type: "+typ)
}

// subsonicAlbumLists — другие списки (subsonic_radio.go: byGenre).
var subsonicAlbumLists = map[string]func(*subCtx) ([]deezer.Album, error){}

// frequentAlbums — альбомы, которые человек слушает чаще всего.
func (c *subCtx) frequentAlbums() ([]deezer.Album, error) {
	rows, err := c.s.DB.Query(
		`SELECT (array_agg(meta ORDER BY played_at DESC))[1]
		   FROM plays WHERE user_id = $1 AND coalesce(meta->>'album_id', '') <> ''
		  GROUP BY meta->>'album_id' ORDER BY count(*) DESC LIMIT 200`, c.userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var albums []deezer.Album
	for rows.Next() {
		var meta []byte
		var t deezer.Track
		if rows.Scan(&meta) == nil && json.Unmarshal(meta, &t) == nil {
			albums = append(albums, deezer.Album{ID: t.AlbumID, Title: t.Album, CoverURL: t.CoverURL, ArtistID: t.ArtistID, ArtistName: t.Artist})
		}
	}
	c.s.overlayFor(nil, albumPtrs(albums), nil)
	return albums, rows.Err()
}

func subsonicAlbumList(c *subCtx, id3 bool) error {
	typ := c.param("type")
	if typ == "" {
		return subMissing("type")
	}
	albums, err := c.albumListOf(typ)
	if err != nil {
		return err
	}
	albums = page(albums, c.intParam("offset", 0, 1<<20), c.intParam("size", 10, 500))
	if id3 {
		return c.ok("albumList2", struct {
			Albums []subAlbumID3 `xml:"album" json:"album,omitempty"`
		}{c.albums(albums)})
	}
	return c.ok("albumList", struct {
		Albums []subSong `xml:"album" json:"album,omitempty"`
	}{c.albumDirs(albums)})
}

// --- избранное ---

func subsonicStarred(c *subCtx, id3 bool) error {
	lib, err := c.library()
	if err != nil {
		return err
	}
	artists := c.artists(lib.followed, lib.counts)
	songs := c.songs(lib.liked)
	if id3 {
		return c.ok("starred2", struct {
			Artists []subArtistID3 `xml:"artist" json:"artist,omitempty"`
			Albums  []subAlbumID3  `xml:"album" json:"album,omitempty"`
			Songs   []subSong      `xml:"song" json:"song,omitempty"`
		}{artists, c.albums(lib.albums), songs})
	}
	return c.ok("starred", struct {
		Artists []subArtistPlain `xml:"artist" json:"artist,omitempty"`
		Albums  []subSong        `xml:"album" json:"album,omitempty"`
		Songs   []subSong        `xml:"song" json:"song,omitempty"`
	}{artistsPlain(artists), c.albumDirs(lib.albums), songs})
}

// star/unstar: id — трек (или альбом, артист с префиксом), albumId, artistId.
func subsonicStar(c *subCtx, on bool) error {
	type target struct{ kind, id string }
	var targets []target
	for _, id := range c.params("id") {
		kind, raw := parseSubID(id)
		targets = append(targets, target{kind, raw})
	}
	for _, id := range c.params("albumId") {
		_, raw := parseSubID(id)
		targets = append(targets, target{subAlbum, raw})
	}
	for _, id := range c.params("artistId") {
		_, raw := parseSubID(id)
		targets = append(targets, target{subArtist, raw})
	}
	if len(targets) == 0 {
		return subMissing("id")
	}
	for _, t := range targets {
		if !validCatalogID(t.id) {
			return subFail(subErrNotFound, "no such item: "+t.kind+t.id)
		}
		var h http.HandlerFunc
		switch {
		case t.kind == subTrack && on:
			h = c.s.handleLike
		case t.kind == subTrack:
			h = c.s.handleUnlike
		case t.kind == subAlbum && on:
			h = c.s.handleSaveAlbum
		case t.kind == subAlbum:
			h = c.s.handleUnsaveAlbum
		case t.kind == subArtist && on:
			h = c.s.handleFollow
		case t.kind == subArtist:
			h = c.s.handleUnfollow
		default:
			return subFail(subErrNotFound, "no such item: "+t.kind+t.id)
		}
		if err := c.call(h, "PUT", "/", map[string]string{"id": t.id}, nil, nil); err != nil {
			return err
		}
	}
	return c.ok("", nil)
}

// --- поиск ---

// search3/search2: по каталогу Deezer и своим релизам. Пустой запрос —
// вся медиатека: так её выкачивают себе Symfonium и другие.
func subsonicSearch(c *subCtx, id3 bool) error {
	q := strings.Trim(strings.TrimSpace(c.param("query")), `"`)
	var tracks []deezer.Track
	var albums []deezer.Album
	var artists []deezer.Artist
	counts := map[string]int{}
	if q == "" || q == "*" {
		lib, err := c.library()
		if err != nil {
			return err
		}
		tracks, albums, artists, counts = lib.liked, lib.albums, lib.artists, lib.counts
	} else {
		var found struct {
			Tracks  []deezer.Track  `json:"tracks"`
			Artists []deezer.Artist `json:"artists"`
			Albums  []deezer.Album  `json:"albums"`
		}
		if err := c.call(c.s.handleSearch, "GET", "/?q="+url.QueryEscape(q), nil, nil, &found); err != nil {
			return err
		}
		tracks, albums, artists = found.Tracks, found.Albums, found.Artists
	}
	artists = page(artists, c.intParam("artistOffset", 0, 1<<20), c.intParam("artistCount", 20, 500))
	albums = page(albums, c.intParam("albumOffset", 0, 1<<20), c.intParam("albumCount", 20, 500))
	tracks = page(tracks, c.intParam("songOffset", 0, 1<<20), c.intParam("songCount", 20, 500))
	a := c.artists(artists, counts)
	if id3 {
		return c.ok("searchResult3", struct {
			Artists []subArtistID3 `xml:"artist" json:"artist,omitempty"`
			Albums  []subAlbumID3  `xml:"album" json:"album,omitempty"`
			Songs   []subSong      `xml:"song" json:"song,omitempty"`
		}{a, c.albums(albums), c.songs(tracks)})
	}
	return c.ok("searchResult2", struct {
		Artists []subArtistPlain `xml:"artist" json:"artist,omitempty"`
		Albums  []subSong        `xml:"album" json:"album,omitempty"`
		Songs   []subSong        `xml:"song" json:"song,omitempty"`
	}{artistsPlain(a), c.albumDirs(albums), c.songs(tracks)})
}
