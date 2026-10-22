package api

import (
	"database/sql"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"slices"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// Свои релизы (загрузка, uploads.go) в тех же ответах, что и каталог Deezer:
// страницы альбома и артиста, трек, медиатека, поиск. ID у них «k…»
// (helpers.go), строки помечены edited/cover_custom/picture_custom, так что
// название, обложку, исполнителей и explicit подставляет overlayFor, как
// правки админа. Сборщики здесь задают остальное (длительность, номера,
// число треков) и решают, видно ли зрителю черновик.

// errNotFound — своего объекта нет или зрителю его не видно (чужой черновик).
var errNotFound = errors.New("not found")

// viewer — кто смотрит.
type viewer struct {
	userID    string
	admin     bool // может править всё
	adminMode bool // админ в режиме админа: видит черновики и скрытое
}

func viewerOf(r *http.Request) viewer {
	admin, _ := r.Context().Value(isAdminCtxKey).(bool)
	return viewer{userID: currentUser(r), admin: admin, adminMode: showHidden(r)}
}

func (v viewer) owns(owner string) bool { return owner != "" && owner == v.userID }

// sees: черновик видят автор и админ в режиме админа.
func (v viewer) sees(draft bool, owner string) bool { return !draft || v.owns(owner) || v.adminMode }

// mayEdit: свой релиз или артиста правят автор и админ.
func (v viewer) mayEdit(owner string) bool { return v.owns(owner) || v.admin }

// editable: показывать ли кнопку правки — автору всегда, админу в режиме
// админа (как остальные его инструменты).
func (v viewer) editable(owner string) bool { return v.owns(owner) || v.adminMode }

// writeCatalogError — как writeDeezerError, но и для своих релизов.
func writeCatalogError(w http.ResponseWriter, prefix string, err error) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, prefix+": not found")
		return
	}
	writeDeezerError(w, prefix, err)
}

// localRelease — то, что знает о своём релизе только наша база.
type localRelease struct {
	libraryID string
	owner     string // "" — автора нет (удалён)
	draft     bool
}

// localAlbum is an own release with its tracklist, as the album page shows
// it. A draft the viewer may not see is errNotFound; one they may see is not
// marked hidden (it would be greyed out for its own author).
func (s *Server) localAlbum(v viewer, id string) (*deezer.Album, []deezer.Track, localRelease, error) {
	var rel localRelease
	a := deezer.Album{ID: id}
	var cover string
	var list []string
	var discs []sql.NullInt64
	err := s.DB.QueryRow(
		`SELECT id, coalesce(owner_id::text, ''), draft, title, coalesce(to_char(released_at, 'YYYY-MM-DD'), ''),
		        coalesce(record_type, ''), coalesce(cover_key, ''), tracklist, tracklist_discs
		   FROM albums WHERE deezer_id = $1`, id).
		Scan(&rel.libraryID, &rel.owner, &rel.draft, &a.Title, &a.ReleaseDate, &a.RecordType, &cover, pq.Array(&list), pq.Array(&discs))
	if errors.Is(err, sql.ErrNoRows) || err == nil && !v.sees(rel.draft, rel.owner) {
		return nil, nil, rel, errNotFound
	}
	if err != nil {
		return nil, nil, rel, err
	}
	if cover != "" {
		a.CoverURL = "/media/" + cover
	}
	rows, err := s.DB.Query(
		`SELECT deezer_id, title, coalesce(track_no, 0), coalesce(disc_no, 1), duration_ms
		   FROM tracks WHERE album_id = $1
		  ORDER BY disc_no NULLS FIRST, track_no NULLS LAST, created_at`, rel.libraryID)
	if err != nil {
		return nil, nil, rel, err
	}
	defer rows.Close()
	tracks := []deezer.Track{}
	for rows.Next() {
		t := deezer.Track{AlbumID: id, Album: a.Title, CoverURL: a.CoverURL}
		if err := rows.Scan(&t.ID, &t.Title, &t.TrackNo, &t.DiscNo, &t.DurationMs); err != nil {
			return nil, nil, rel, err
		}
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, rel, err
	}
	s.overlayFor(nil, []*deezer.Album{&a}, tracks)
	if list != nil {
		tracks = s.arrangeOwn(v, &a, tracks, list, discs)
	}
	a.NbTracks = len(tracks)
	for _, t := range tracks {
		a.DurationSec += t.DurationMs / 1000
	}
	a.Explicit = slices.ContainsFunc(tracks, func(t deezer.Track) bool { return t.Explicit })
	if rel.draft { // видит автор или админ — не серым
		a.Hidden = false
		for i := range tracks {
			tracks[i].Hidden = false
		}
	}
	return &a, tracks, rel, nil
}

// arrangeOwn puts an own release's tracks in its tracklist's order
// (albums.tracklist: own tracks and tracks from other releases — Deezer or
// own — added in the upload editor). An added track keeps its ID, audio and
// album, but shows under this release's title and cover, as on a Deezer
// album with an edited tracklist (tracklist.go). Own tracks not in the list
// (uploaded after the last save) go to the end; a track that's gone, or a
// stranger's draft, is skipped. discs — the disc of each place in list
// (albums.tracklist_discs); a place without one stays on the disc before
// it. Numbers follow the order within each disc.
func (s *Server) arrangeOwn(v viewer, a *deezer.Album, own []deezer.Track, list []string, discs []sql.NullInt64) []deezer.Track {
	byID := make(map[string]deezer.Track, len(own))
	for _, t := range own {
		byID[t.ID] = t
	}
	out := make([]deezer.Track, 0, len(list)+len(own))
	placed := map[string]bool{}
	disc := 1
	for k, id := range list {
		if placed[id] {
			continue
		}
		placed[id] = true
		if k < len(discs) && discs[k].Valid && discs[k].Int64 > 0 {
			disc = int(discs[k].Int64)
		}
		if t, ok := byID[id]; ok {
			t.DiscNo = disc
			out = append(out, t)
			continue
		}
		t, err := s.catalogTrack(v, id)
		if err != nil {
			if !errors.Is(err, errNotFound) {
				log.Printf("tracklist of %s: track %s: %v", a.ID, id, err)
			}
			continue
		}
		t.Album, t.CoverURL, t.DiscNo = a.Title, a.CoverURL, disc
		out = append(out, *t)
	}
	for _, t := range own {
		if !placed[t.ID] {
			t.DiscNo = max(t.DiscNo, disc) // загружен на последний диск (uploads_audio.go)
			out = append(out, t)
		}
	}
	next := map[int]int{}
	for i := range out {
		next[out[i].DiscNo]++
		out[i].TrackNo = next[out[i].DiscNo]
	}
	return out
}

// localAlbumCards — свои релизы карточками (дискография, медиатека), в
// порядке ids; чего нет — пропускается. Черновики помечены скрытыми
// (overlay.go), списки каталога их отбрасывают сами. Треки, добавленные с
// других релизов (arrangeOwn), идут в число треков, длительность и метку
// explicit (как на странице релиза: хоть один трек explicit).
func (s *Server) localAlbumCards(ids []string) ([]deezer.Album, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT a.deezer_id, a.title, coalesce(to_char(a.released_at, 'YYYY-MM-DD'), ''), coalesce(a.record_type, ''),
		        coalesce(a.cover_key, ''), count(t.id), coalesce(sum(t.duration_ms), 0), coalesce(bool_or(e.explicit), false),
		        ARRAY(SELECT x FROM unnest(a.tracklist) x
		               WHERE NOT EXISTS (SELECT 1 FROM tracks o WHERE o.album_id = a.id AND o.deezer_id = x))
		   FROM albums a LEFT JOIN tracks t ON t.album_id = a.id
		        LEFT JOIN track_overrides e ON e.deezer_id = t.deezer_id
		  WHERE a.deezer_id = ANY($1)
		  GROUP BY a.id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]deezer.Album{}
	added := map[string][]string{}
	var all []string
	for rows.Next() {
		var a deezer.Album
		var cover string
		var ms int
		var other []string
		if err := rows.Scan(&a.ID, &a.Title, &a.ReleaseDate, &a.RecordType, &cover, &a.NbTracks, &ms, &a.Explicit, pq.Array(&other)); err != nil {
			return nil, err
		}
		if cover != "" {
			a.CoverURL = "/media/" + cover
		}
		a.NbTracks += len(other)
		a.DurationSec = ms / 1000
		byID[a.ID] = a
		added[a.ID] = other
		all = append(all, other...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(all) > 0 {
		dur := s.trackDurations(all)
		explicit := s.explicitTracks(all)
		for id, other := range added {
			a := byID[id]
			for _, t := range other {
				a.DurationSec += dur[t] / 1000
				a.Explicit = a.Explicit || explicit[t]
			}
			byID[id] = a
		}
	}
	out := make([]deezer.Album, 0, len(byID))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	s.overlayFor(nil, albumPtrs(out), nil)
	return out, nil
}

// trackDurations — длительность треков (ms по ID): из базы (свои и
// скачанные), остальные — из Deezer (кеш). Чего не узнать — нет в ответе.
func (s *Server) trackDurations(ids []string) map[string]int {
	out := map[string]int{}
	rows, err := s.DB.Query(`SELECT deezer_id, duration_ms FROM tracks WHERE deezer_id = ANY($1) AND duration_ms > 0`, pq.Array(ids))
	if err == nil {
		for rows.Next() {
			var id string
			var ms int
			if rows.Scan(&id, &ms) == nil {
				out[id] = ms
			}
		}
		rows.Close()
	}
	for _, id := range ids {
		if _, ok := out[id]; ok || isLocalID(id) {
			continue
		}
		if t, err := s.Deezer.GetTrack(id); err == nil {
			out[id] = t.DurationMs
		}
	}
	return out
}

// explicitTracks — какие из треков explicit: правка (track_overrides), иначе
// метка Deezer (кеш). Своему треку без правки метки нет.
func (s *Server) explicitTracks(ids []string) map[string]bool {
	out := map[string]bool{}
	edited := map[string]bool{}
	rows, err := s.DB.Query(`SELECT deezer_id, explicit FROM track_overrides WHERE deezer_id = ANY($1) AND explicit IS NOT NULL`, pq.Array(ids))
	if err == nil {
		for rows.Next() {
			var id string
			var explicit bool
			if rows.Scan(&id, &explicit) == nil {
				out[id], edited[id] = explicit, true
			}
		}
		rows.Close()
	}
	for _, id := range ids {
		if edited[id] || isLocalID(id) {
			continue
		}
		if t, err := s.Deezer.GetTrack(id); err == nil {
			out[id] = t.Explicit
		}
	}
	return out
}

// localTracks — свои треки по ID, в том же порядке; чужие черновики и то,
// чего нет, пропускаются.
func (s *Server) localTracks(v viewer, ids []string) ([]deezer.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT t.deezer_id, t.title, coalesce(t.track_no, 0), coalesce(t.disc_no, 1), t.duration_ms,
		        a.deezer_id, a.title, coalesce(a.cover_key, ''), a.draft, coalesce(a.owner_id::text, '')
		   FROM tracks t JOIN albums a ON a.id = t.album_id
		  WHERE t.deezer_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]deezer.Track{}
	for rows.Next() {
		var t deezer.Track
		var cover, owner string
		var draft bool
		if err := rows.Scan(&t.ID, &t.Title, &t.TrackNo, &t.DiscNo, &t.DurationMs, &t.AlbumID, &t.Album, &cover, &draft, &owner); err != nil {
			return nil, err
		}
		if !v.sees(draft, owner) {
			continue
		}
		if cover != "" {
			t.CoverURL = "/media/" + cover
		}
		byID[t.ID] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]deezer.Track, 0, len(byID))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	s.overlayFor(nil, nil, out)
	return out, nil
}

// localArtist — свой артист (k…). Черновиков у артистов нет: его видно,
// даже пока у него нет ни одного релиза.
func (s *Server) localArtist(id string) (*deezer.Artist, string, error) {
	a := deezer.Artist{ID: id}
	var owner string
	err := s.DB.QueryRow(`SELECT name, coalesce(owner_id::text, '') FROM artists WHERE deezer_id = $1`, id).Scan(&a.Name, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", errNotFound
	}
	if err != nil {
		return nil, "", err
	}
	s.overlayFor([]*deezer.Artist{&a}, nil, nil)
	return &a, owner, nil
}

// localArtistTracks — треки релизов, где он исполнитель (новые релизы
// первыми), без черновиков.
func (s *Server) localArtistTracks(v viewer, artistID string, limit int) ([]deezer.Track, error) {
	rows, err := s.DB.Query(
		`SELECT t.deezer_id FROM tracks t JOIN albums a ON a.id = t.album_id
		  WHERE NOT a.draft AND NOT a.hidden
		    AND (a.artist_ids @> ARRAY[$1]
		         OR EXISTS (SELECT 1 FROM track_overrides o WHERE o.deezer_id = t.deezer_id AND o.artist_ids @> ARRAY[$1]))
		  ORDER BY a.released_at DESC NULLS LAST, a.created_at DESC, t.disc_no, t.track_no
		  LIMIT $2`, artistID, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.localTracks(v, ids)
}

// --- Deezer или свой каталог ---

// catalogTrack — трек со всеми правками, откуда бы он ни был.
func (s *Server) catalogTrack(v viewer, id string) (*deezer.Track, error) {
	if isLocalID(id) {
		tracks, err := s.localTracks(v, []string{id})
		if err != nil {
			return nil, err
		}
		if len(tracks) == 0 {
			return nil, errNotFound
		}
		return &tracks[0], nil
	}
	t, err := s.Deezer.GetTrack(id)
	if err != nil {
		return nil, err
	}
	tracks := []deezer.Track{*t}
	s.overlayFor(nil, nil, tracks)
	return &tracks[0], nil
}

// catalogTracks — треки по ID в том же порядке. Треки Deezer — без правок
// (их накладывает вызывающий, как и раньше), свои — уже с правками.
// Любая ошибка Deezer — ошибка всего списка; своего трека нет — errNotFound.
func (s *Server) catalogTracks(v viewer, ids []string) ([]deezer.Track, error) {
	var dz, mine []string
	for _, id := range ids {
		if isLocalID(id) {
			mine = append(mine, id)
		} else {
			dz = append(dz, id)
		}
	}
	byID := map[string]deezer.Track{}
	if len(dz) > 0 {
		got, err := s.Deezer.GetTracks(dz)
		if err != nil {
			return nil, err
		}
		for _, t := range got {
			byID[t.ID] = *t
		}
	}
	own, err := s.localTracks(v, mine)
	if err != nil {
		return nil, err
	}
	if len(own) != len(mine) {
		return nil, errNotFound
	}
	for _, t := range own {
		byID[t.ID] = t
	}
	out := make([]deezer.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// catalogAlbum — карточка альбома (без треклиста).
func (s *Server) catalogAlbum(v viewer, id string) (*deezer.Album, error) {
	if isLocalID(id) {
		a, _, _, err := s.localAlbum(v, id)
		return a, err
	}
	a, err := s.Deezer.GetAlbum(id)
	if err != nil {
		return nil, err
	}
	s.overlayFor(nil, []*deezer.Album{a}, nil)
	return a, nil
}

// catalogArtist — артист (имя, фото) с правками.
func (s *Server) catalogArtist(id string) (*deezer.Artist, error) {
	if isLocalID(id) {
		a, _, err := s.localArtist(id)
		return a, err
	}
	a, err := s.artistInfo(id)
	if err != nil {
		return nil, err
	}
	s.overlayFor([]*deezer.Artist{&a}, nil, nil)
	return &a, nil
}

// --- страницы своего артиста ---

// localArtistPage — то же, что handleDiscoverArtist, для своего артиста:
// его релизы из базы, «Популярное» — прослушивания здесь, потом его треки.
func (s *Server) localArtistPage(w http.ResponseWriter, r *http.Request, id string) {
	v := viewerOf(r)
	artist, owner, err := s.localArtist(id)
	if err != nil {
		writeCatalogError(w, "artist lookup failed", err)
		return
	}
	albums := s.creditedDiscography(id, nil)
	played, _ := s.artistTopByPlays(id)
	own, err := s.localArtistTracks(v, id, topLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "artist tracks lookup failed")
		return
	}
	top := popularTracks(played, own)
	s.overlayFor(nil, nil, top)
	if albums == nil {
		albums = []deezer.Album{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artist": artist, "albums": visibleAlbums(r, albums), "top": visibleTracks(r, top),
		"editable": v.editable(owner),
	})
}

// localArtistRadio — «радио» своего артиста: похожих у нас не знает никто,
// так что это его треки вперемешку.
func (s *Server) localArtistRadio(w http.ResponseWriter, r *http.Request, id string) {
	tracks, err := s.localArtistTracks(viewerOf(r), id, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "artist tracks lookup failed")
		return
	}
	rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
	if tracks == nil {
		tracks = []deezer.Track{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

// deezerRowOnly guards the admin's tools for catalogue rows by library ID
// (reset to Deezer, credits and tracklist edits, deletion that leaves the
// library alone): own uploads have their own editor and deletion
// (uploads.go). table is one of the catalogue tables, never user input.
func (s *Server) deezerRowOnly(table string, next http.HandlerFunc) http.HandlerFunc {
	query := `SELECT deezer_id FROM ` + table + ` WHERE id::text = $1`
	return func(w http.ResponseWriter, r *http.Request) {
		var dz string
		if err := s.DB.QueryRow(query, r.PathValue("id")).Scan(&dz); err == nil && isLocalID(dz) {
			writeError(w, http.StatusBadRequest, "own uploads are edited in the upload editor")
			return
		}
		next(w, r)
	}
}

// --- поиск по своим ---

type localResults struct {
	tracks  []deezer.Track
	artists []deezer.Artist
	albums  []deezer.Album
}

func (l localResults) empty() bool {
	return len(l.tracks) == 0 && len(l.artists) == 0 && len(l.albums) == 0
}

// searchLocal ищет свои релизы, треки и артистов по подстроке названия
// (без учёта регистра; своего каталога немного, индекс не нужен).
// Черновики не ищутся.
func (s *Server) searchLocal(v viewer, q string, limit int) (localResults, error) {
	var out localResults
	ids := func(query string) ([]string, error) {
		rows, err := s.DB.Query(query, q, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	albumIDs, err := ids(`SELECT deezer_id FROM albums
		WHERE deezer_id LIKE 'k%' AND NOT draft AND NOT hidden
		  AND strpos(lower(title), lower($1)) > 0
		ORDER BY released_at DESC NULLS LAST LIMIT $2`)
	if err != nil {
		return out, err
	}
	if out.albums, err = s.localAlbumCards(albumIDs); err != nil {
		return out, err
	}
	trackIDs, err := ids(`SELECT t.deezer_id FROM tracks t JOIN albums a ON a.id = t.album_id
		WHERE t.deezer_id LIKE 'k%' AND NOT a.draft AND NOT a.hidden AND strpos(lower(t.title), lower($1)) > 0
		ORDER BY a.released_at DESC NULLS LAST, t.track_no LIMIT $2`)
	if err != nil {
		return out, err
	}
	if out.tracks, err = s.localTracks(v, trackIDs); err != nil {
		return out, err
	}
	out.artists, err = s.searchOwnArtists(v, q, limit, false)
	return out, err
}

// searchOwnArtists — свои артисты по подстроке имени. В общем поиске — те, у
// кого есть опубликованный релиз; при выборе исполнителей (picker) — ещё и
// созданные этим человеком, пока без релизов.
func (s *Server) searchOwnArtists(v viewer, q string, limit int, picker bool) ([]deezer.Artist, error) {
	rows, err := s.DB.Query(`
		SELECT ar.deezer_id FROM artists ar
		 WHERE ar.deezer_id LIKE 'k%' AND strpos(lower(ar.name), lower($1)) > 0
		   AND (EXISTS (SELECT 1 FROM albums a WHERE a.artist_ids @> ARRAY[ar.deezer_id] AND NOT a.draft AND NOT a.hidden)
		        OR $3 AND (ar.owner_id::text = $4 OR $5))
		 ORDER BY lower(ar.name) = lower($1) DESC, ar.name
		 LIMIT $2`, q, limit, picker, v.userID, v.admin)
	if err != nil {
		return nil, err
	}
	var artists []deezer.Artist
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		artists = append(artists, deezer.Artist{ID: id})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.overlayFor(artistPtrs(artists), nil, nil) // имя и фото (edited, picture_custom)
	return artists, nil
}
