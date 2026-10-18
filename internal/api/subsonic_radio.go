package api

import (
	"encoding/json"
	"math/rand/v2"
	"net/url"
	"strings"

	"kaikku/internal/deezer"
)

// Радио, топ артиста, случайное, сведения об артисте и жанры — для «микса»
// и страниц артиста в приложениях.

func init() {
	subsonicRoute("getSimilarSongs2", func(c *subCtx) error { return subsonicSimilar(c, "similarSongs2") })
	subsonicRoute("getSimilarSongs", func(c *subCtx) error { return subsonicSimilar(c, "similarSongs") })
	subsonicRoute("getTopSongs", subsonicTopSongs)
	subsonicRoute("getRandomSongs", subsonicRandomSongs)
	subsonicRoute("getArtistInfo2", func(c *subCtx) error { return subsonicArtistInfo(c, true) })
	subsonicRoute("getArtistInfo", func(c *subCtx) error { return subsonicArtistInfo(c, false) })
	subsonicRoute("getAlbumInfo2", subsonicAlbumInfo)
	subsonicRoute("getAlbumInfo", subsonicAlbumInfo)
	subsonicRoute("getGenres", subsonicGenres)
	subsonicRoute("getSongsByGenre", subsonicSongsByGenre)
	subsonicAlbumLists["byGenre"] = genreAlbums
}

type subSongs struct {
	Songs []subSong `xml:"song" json:"song,omitempty"`
}

// similar — радио артиста (как автопродолжение на сайте). id — артист,
// трек или альбом: для них — радио их исполнителя.
func subsonicSimilar(c *subCtx, key string) error {
	id := c.param("id")
	if id == "" {
		return subMissing("id")
	}
	kind, raw := parseSubID(id)
	if !validCatalogID(raw) {
		return subFail(subErrNotFound, "no such item: "+id)
	}
	artist := raw
	switch kind {
	case subTrack:
		t, err := c.s.catalogTrack(viewerOf(c.r), raw)
		if err != nil {
			return subFail(subErrNotFound, "song not found")
		}
		artist = t.ArtistID
	case subAlbum:
		a := c.s.albumCard(raw)
		if a == nil {
			return subFail(subErrNotFound, "album not found")
		}
		artist = a.ArtistID
	case subArtist:
	default:
		return subFail(subErrNotFound, "no such item: "+id)
	}
	var out struct {
		Tracks []deezer.Track `json:"tracks"`
	}
	if err := c.call(c.s.handleDiscoverRadio, "GET", "/", map[string]string{"id": artist}, nil, &out); err != nil {
		return err
	}
	return c.ok(key, subSongs{page(c.songs(out.Tracks), 0, c.intParam("count", 50, 500))})
}

// getTopSongs: artist — имя; берётся артист поиска с таким именем.
func subsonicTopSongs(c *subCtx) error {
	name := strings.TrimSpace(c.param("artist"))
	if name == "" {
		return subMissing("artist")
	}
	var found struct {
		Artists []deezer.Artist `json:"artists"`
	}
	if err := c.call(c.s.handleSearch, "GET", "/?q="+url.QueryEscape(name), nil, nil, &found); err != nil || len(found.Artists) == 0 {
		return c.ok("topSongs", subSongs{})
	}
	artist := found.Artists[0]
	for _, a := range found.Artists {
		if strings.EqualFold(a.Name, name) {
			artist = a
			break
		}
	}
	p, err := c.artistPage(artist.ID)
	if err != nil {
		return err
	}
	return c.ok("topSongs", subSongs{page(c.songs(p.Top), 0, c.intParam("count", 50, 500))})
}

// getRandomSongs — любимое и слушанное вперемешку; с genre — из жанра.
func subsonicRandomSongs(c *subCtx) error {
	size := c.intParam("size", 10, 500)
	var pool []deezer.Track
	if g := c.param("genre"); g != "" {
		p, err := c.genre(g)
		if err != nil {
			return err
		}
		pool = append(p.Own, p.Tracks...)
	} else {
		var err error
		if pool, err = c.listenedTracks(); err != nil {
			return err
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return c.ok("randomSongs", subSongs{page(c.songs(pool), 0, size)})
}

// listenedTracks — любимые и слушанные за 90 дней, без повторов.
func (c *subCtx) listenedTracks() ([]deezer.Track, error) {
	rows, err := c.s.DB.Query(
		`SELECT meta FROM liked_tracks WHERE user_id = $1
		 UNION ALL
		 SELECT (array_agg(meta ORDER BY played_at DESC))[1] FROM plays
		  WHERE user_id = $1 AND played_at > now() - interval '90 days' GROUP BY deezer_id`, c.userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var tracks []deezer.Track
	for rows.Next() {
		var meta []byte
		var t deezer.Track
		if rows.Scan(&meta) == nil && json.Unmarshal(meta, &t) == nil && !seen[t.ID] {
			seen[t.ID] = true
			tracks = append(tracks, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	c.s.overlayFor(nil, nil, tracks)
	return tracks, nil
}

// --- сведения об артисте и альбоме ---

type subImages struct {
	Small  string `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	Medium string `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	Large  string `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
}

// imagesOf — картинка в трёх размерах по полному адресу сайта.
func (s *Server) imagesOf(u string) subImages {
	at := func(px int) string {
		if kind, hash, file, ok := deezerImageAt(u, px); ok {
			return strings.TrimRight(s.PublicURL, "/") + "/api/img/" + kind + "/" + hash + "/" + file
		}
		return s.publicImageURL(u)
	}
	if u == "" {
		return subImages{}
	}
	return subImages{at(250), at(500), at(1000)}
}

// bioLangOf — язык описания: русский, если приложение просит его.
func bioLangOf(c *subCtx) string {
	if strings.HasPrefix(strings.ToLower(c.r.Header.Get("Accept-Language")), "ru") {
		return "ru"
	}
	return "en"
}

func subsonicArtistInfo(c *subCtx, id3 bool) error {
	id, err := c.catalogParam("id", subArtist)
	if err != nil {
		return err
	}
	var bio struct {
		Text string `json:"text"`
		URL  string `json:"url"`
	}
	if err := c.call(c.s.handleArtistBio, "GET", "/?lang="+bioLangOf(c), map[string]string{"id": id}, nil, &bio); err != nil {
		return err
	}
	var related struct {
		Artists []deezer.Artist `json:"artists"`
	}
	c.call(c.s.handleDiscoverRelated, "GET", "/", map[string]string{"id": id}, nil, &related) // нет похожих — не беда
	similar := c.artists(page(related.Artists, 0, c.intParam("count", 20, 100)), nil)

	picture, _ := c.coverURL(subArtist + id)
	images := c.s.imagesOf(picture)
	lastFm := ""
	if strings.Contains(bio.URL, "last.fm") {
		lastFm = bio.URL
	}
	if id3 {
		return c.ok("artistInfo2", struct {
			Biography string `xml:"biography,omitempty" json:"biography,omitempty"`
			LastFmURL string `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
			subImages
			Similar []subArtistID3 `xml:"similarArtist" json:"similarArtist,omitempty"`
		}{bio.Text, lastFm, images, similar})
	}
	return c.ok("artistInfo", struct {
		Biography string `xml:"biography,omitempty" json:"biography,omitempty"`
		LastFmURL string `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
		subImages
		Similar []subArtistPlain `xml:"similarArtist" json:"similarArtist,omitempty"`
	}{bio.Text, lastFm, images, artistsPlain(similar)})
}

// getAlbumInfo — только обложки: своих заметок об альбомах у нас нет.
func subsonicAlbumInfo(c *subCtx) error {
	id, err := c.catalogParam("id", subAlbum)
	if err != nil {
		return err
	}
	u, err := c.coverURL(subAlbum + id)
	if err != nil {
		return err
	}
	return c.ok("albumInfo", c.s.imagesOf(u))
}

// --- жанры (теги Last.fm, genres.go) ---

func subsonicGenres(c *subCtx) error {
	var out struct {
		Genres []genre `json:"genres"`
	}
	if err := c.call(c.s.handleGenres, "GET", "/", nil, nil, &out); err != nil {
		return err
	}
	type subGenre struct {
		SongCount  int    `xml:"songCount,attr" json:"songCount"`
		AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
		Value      string `xml:",chardata" json:"value"`
	}
	var list []subGenre
	for _, g := range out.Genres {
		list = append(list, subGenre{Value: g.Name})
	}
	return c.ok("genres", struct {
		Genres []subGenre `xml:"genre" json:"genre,omitempty"`
	}{list})
}

// genre — страница жанра по названию (как его отдал getGenres).
func (c *subCtx) genre(name string) (genrePage, error) {
	var p genrePage
	slug := tagSlug(name)
	if slug == "" {
		return p, subFail(subErrNotFound, "no such genre: "+name)
	}
	err := c.call(c.s.handleGenre, "GET", "/", map[string]string{"slug": slug}, nil, &p)
	return p, err
}

func subsonicSongsByGenre(c *subCtx) error {
	name := c.param("genre")
	if name == "" {
		return subMissing("genre")
	}
	p, err := c.genre(name)
	if err != nil {
		return err
	}
	songs := page(c.songs(append(p.Own, p.Tracks...)), c.intParam("offset", 0, 1<<20), c.intParam("count", 10, 500))
	return c.ok("songsByGenre", subSongs{songs})
}

// genreAlbums — альбомы треков жанра (getAlbumList2?type=byGenre).
func genreAlbums(c *subCtx) ([]deezer.Album, error) {
	name := c.param("genre")
	if name == "" {
		return nil, subMissing("genre")
	}
	p, err := c.genre(name)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var albums []deezer.Album
	for _, t := range append(p.Own, p.Tracks...) {
		if t.AlbumID != "" && !seen[t.AlbumID] && !t.Hidden {
			seen[t.AlbumID] = true
			albums = append(albums, deezer.Album{ID: t.AlbumID, Title: t.Album, CoverURL: t.CoverURL, ArtistID: t.ArtistID, ArtistName: t.Artist})
		}
	}
	return albums, nil
}
