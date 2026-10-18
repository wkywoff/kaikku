package api

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// maxDiscography caps how many releases an artist page loads (Deezer pages
// them by 100, so this is at most 5 requests).
const maxDiscography = 500

// Артист и его топ меняются редко, а открываются часто и подряд:
// страница артиста → «Показать все» → альбом → «Ещё альбомы». Дискография
// — чаще: новый релиз должен появиться на странице в пределах часа.
const (
	artistTTL      = 24 * time.Hour
	discographyTTL = time.Hour
)

func (s *Server) artistInfo(id string) (deezer.Artist, error) {
	return storedCached(s, "artist:"+id, artistTTL, func() (deezer.Artist, error) {
		a, err := s.Deezer.GetArtist(id)
		if err != nil {
			return deezer.Artist{}, err
		}
		return *a, nil
	})
}

func (s *Server) artistDiscography(id string) ([]deezer.Album, error) {
	return storedCached(s, "discography:"+id, discographyTTL, func() ([]deezer.Album, error) {
		return s.Deezer.GetArtistAlbums(id, maxDiscography)
	})
}

func (s *Server) artistTop(id string) ([]deezer.Track, error) {
	return storedCached(s, "top:"+id, artistTTL, func() ([]deezer.Track, error) {
		return s.Deezer.GetArtistTop(id, topLimit)
	})
}

// «Популярное» у артиста — по прослушиваниям на нашем сервисе за месяц
// (топ Deezer — старые фиты и не то, что тут слушают): треки, где он
// исполнитель, основной или фит. Не набралось — дальше топ Deezer.
const (
	topLimit  = 10
	topWindow = "30 days"
)

func (s *Server) artistTopByPlays(id string) ([]deezer.Track, error) {
	rows, err := s.DB.Query(`
		SELECT (array_agg(meta ORDER BY played_at DESC))[1]
		  FROM plays
		 WHERE played_at > now() - $3::interval
		   AND (meta->>'artist_id' = $1 OR meta->'contributors' @> jsonb_build_array(jsonb_build_object('id', $1::text)))
		 GROUP BY deezer_id
		 ORDER BY count(*) DESC, max(played_at) DESC
		 LIMIT $2`, id, 2*topLimit, topWindow) // с запасом: часть отсеется ниже
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deezer.Track
	for rows.Next() {
		var meta []byte
		var t deezer.Track
		if rows.Scan(&meta) == nil && json.Unmarshal(meta, &t) == nil && t.ID != "" {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Снимок — каким трек был, когда его слушали; исполнителей с тех пор
	// могли поменять (правка админа, автор своего релиза убрал фит).
	// Остаются треки, где артист есть сейчас.
	s.overlayFor(nil, nil, out)
	out = slices.DeleteFunc(out, func(t deezer.Track) bool {
		return t.ArtistID != id && !slices.ContainsFunc(t.Contributors, func(c deezer.ArtistRef) bool { return c.ID == id })
	})
	return out[:min(len(out), topLimit)], nil
}

// popularTracks: сначала слушаемое здесь, потом топ Deezer без повторов.
func popularTracks(played, deezerTop []deezer.Track) []deezer.Track {
	out := played
	for _, t := range deezerTop {
		if len(out) >= topLimit {
			break
		}
		if !slices.ContainsFunc(out, func(x deezer.Track) bool { return x.ID == t.ID }) {
			out = append(out, t)
		}
	}
	return out
}

// handleDiscoverArtist returns one Deezer artist, their top tracks and
// discography in one response — one round-trip per artist page; the three
// Deezer requests run in parallel.
func (s *Server) handleDiscoverArtist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isLocalID(id) {
		s.localArtistPage(w, r, id)
		return
	}
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "invalid deezer id")
		return
	}
	var (
		artist deezer.Artist
		albums []deezer.Album
		top    []deezer.Track
		errs   [3]error
		wg     sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		errs[0] = guard("artist info", func() (err error) { artist, err = s.artistInfo(id); return })
	}()
	go func() {
		defer wg.Done()
		errs[1] = guard("artist albums", func() (err error) { albums, err = s.artistDiscography(id); return })
	}()
	go func() {
		defer wg.Done()
		errs[2] = guard("artist top", func() (err error) { top, err = s.artistTop(id); return })
	}()
	wg.Wait()
	for i, what := range []string{"deezer artist lookup failed", "deezer artist albums lookup failed", "deezer artist top lookup failed"} {
		if errs[i] != nil {
			writeDeezerError(w, what, errs[i])
			return
		}
	}
	played, err := s.artistTopByPlays(id)
	if err != nil {
		log.Printf("artist %s: top by plays: %v", id, err)
	}
	albums, top = s.creditedDiscography(id, cloneAlbums(albums)), popularTracks(played, cloneTracks(top))
	s.overlayFor([]*deezer.Artist{&artist}, albumPtrs(albums), top)
	writeJSON(w, http.StatusOK, map[string]any{"artist": artist, "albums": visibleAlbums(r, albums), "top": visibleTracks(r, top)})
}

// handleDiscoverArtistAlbums is the artist's discography alone — for the
// "More albums by …" rows on an album page, without the top tracks.
func (s *Server) handleDiscoverArtistAlbums(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	var albums []deezer.Album
	if !isLocalID(id) { // у своего артиста всё — из базы (creditedDiscography)
		var err error
		if albums, err = s.artistDiscography(id); err != nil {
			writeDeezerError(w, "deezer artist albums lookup failed", err)
			return
		}
	}
	albums = s.creditedDiscography(id, cloneAlbums(albums))
	if albums == nil {
		albums = []deezer.Album{}
	}
	s.overlayFor(nil, albumPtrs(albums), nil)
	writeJSON(w, http.StatusOK, map[string]any{"albums": visibleAlbums(r, albums)})
}

// handleDiscoverAlbum returns one Deezer album plus its full, correctly
// ordered tracklist — same one-round-trip reasoning as the artist endpoint.
func (s *Server) handleDiscoverAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isLocalID(id) {
		v := viewerOf(r)
		album, tracks, rel, err := s.localAlbum(v, id)
		if err != nil {
			writeCatalogError(w, "album lookup failed", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"album": album, "tracks": tracks, "versions": []deezer.Album{},
			// Карандаш на странице релиза — только в режиме админа (как вся
			// правка на страницах); автор правит свои релизы из медиатеки.
			"draft": rel.draft, "editable": v.adminMode,
		})
		return
	}
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "invalid deezer id")
		return
	}
	album, err := s.Deezer.GetAlbum(id)
	if err != nil {
		writeDeezerError(w, "deezer album lookup failed", err)
		return
	}
	tracks, arranged, err := s.albumTracks(album)
	if err != nil {
		writeDeezerError(w, "deezer album tracks lookup failed", err)
		return
	}
	if fromApp(r) { // enrich.go
		s.enrichLater(s.Deezer.EnrichContributorsCached(tracks))
		guessContributors(album, tracks)
	} else {
		s.Deezer.EnrichContributors(tracks)
	}
	s.overlayFor(nil, []*deezer.Album{album}, tracks)
	if arranged {
		renumber(tracks, album.ID) // свой треклист (tracklist.go) — номера по новому порядку
	}
	for i := range tracks { // треклист берёт название и обложку альбома из album
		tracks[i].Album = album.Title
		tracks[i].CoverURL = album.CoverURL
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": album, "tracks": tracks, "versions": s.albumVersions(r, album)})
}

// handleDiscoverTrack — один трек со всеми исполнителями. Радио, чарт и
// поиск Deezer отдают только основного; плеер дозапрашивает фиты для
// трека, который начал играть (по запросу на трек, а не на весь список).
func (s *Server) handleDiscoverTrack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid track id")
		return
	}
	track, err := s.catalogTrack(viewerOf(r), id)
	if err != nil {
		writeCatalogError(w, "track lookup failed", err)
		return
	}
	writeJSON(w, http.StatusOK, track)
}

// Похожие артисты меняются редко — спрашиваем Deezer не чаще раза в 12 часов.
const relatedTTL = 12 * time.Hour

func (s *Server) relatedArtists(id string) ([]deezer.Artist, error) {
	return storedCached(s, "related:"+id, relatedTTL, func() ([]deezer.Artist, error) {
		return s.Deezer.GetArtistRelated(id, 20)
	})
}

// handleDiscoverRelated — «Похожие исполнители» на странице артиста.
func (s *Server) handleDiscoverRelated(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isLocalID(id) { // похожих на своего артиста никто не знает
		writeJSON(w, http.StatusOK, map[string]any{"artists": []deezer.Artist{}})
		return
	}
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "invalid deezer id")
		return
	}
	related, err := s.relatedArtists(id)
	if err != nil {
		writeDeezerError(w, "deezer related artists lookup failed", err)
		return
	}
	// Копия: правки накладываются на месте, а кеш — ответ Deezer. make, а не
	// append к nil: у артиста без похожих иначе ушло бы null, и страница падала.
	artists := make([]deezer.Artist, len(related))
	copy(artists, related)
	s.overlayFor(artistPtrs(artists), nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists})
}

// handleDiscoverRadio — радио артиста: его треки вперемешку с похожими.
// Каждый раз новая подборка, поэтому без кеша. Им же продолжается музыка,
// когда очередь кончилась и включено автовоспроизведение.
func (s *Server) handleDiscoverRadio(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isLocalID(id) {
		s.localArtistRadio(w, r, id)
		return
	}
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "invalid deezer id")
		return
	}
	tracks, err := s.Deezer.GetArtistRadio(id)
	if err != nil {
		writeDeezerError(w, "deezer artist radio lookup failed", err)
		return
	}
	s.overlayFor(nil, nil, tracks)
	writeJSON(w, http.StatusOK, map[string]any{"tracks": visibleTracks(r, tracks)})
}
