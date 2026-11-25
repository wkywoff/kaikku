package api

import (
	"database/sql"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

var errNotFound = errors.New("not found")

type viewer struct {
	userID    string
	admin     bool
	adminMode bool
}

func viewerOf(r *http.Request) viewer {
	admin, _ := r.Context().Value(isAdminCtxKey).(bool)
	return viewer{userID: currentUser(r), admin: admin, adminMode: showHidden(r)}
}

func (v viewer) owns(owner string) bool { return owner != "" && owner == v.userID }

func (v viewer) sees(draft bool, owner string) bool { return !draft || v.owns(owner) || v.adminMode }

func (v viewer) mayEdit(owner string) bool { return v.owns(owner) || v.admin }

func (v viewer) editable(owner string) bool { return v.owns(owner) || v.adminMode }

func writeCatalogError(w http.ResponseWriter, prefix string, err error) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, prefix+": not found")
		return
	}
	writeDeezerError(w, prefix, err)
}

type localRelease struct {
	libraryID string
	owner     string
	draft     bool
}

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
	if rel.draft {
		a.Hidden = false
		for i := range tracks {
			tracks[i].Hidden = false
		}
	}
	return &a, tracks, rel, nil
}

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
			t.DiscNo = max(t.DiscNo, disc)
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
		closeRows("track durations", rows)
	} else {
		log.Printf("track durations: %v", err)
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
		closeRows("explicit tracks", rows)
	} else {
		log.Printf("explicit tracks: %v", err)
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
		serverError(w, r, "artist tracks lookup failed", err)
		return
	}
	top := popularTracks(played, own)
	s.overlayFor(nil, albumPtrs(albums), top)
	if albums == nil {
		albums = []deezer.Album{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artist": artist, "albums": visibleAlbums(r, albums), "top": visibleTracks(r, top),
		"editable": v.editable(owner),
	})
}

func (s *Server) localArtistRadio(w http.ResponseWriter, r *http.Request, id string) {
	tracks, err := s.localArtistTracks(viewerOf(r), id, 100)
	if err != nil {
		serverError(w, r, "artist tracks lookup failed", err)
		return
	}
	rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
	if tracks == nil {
		tracks = []deezer.Track{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

func (s *Server) deezerRowOnly(table string, next http.HandlerFunc) http.HandlerFunc {
	query := `SELECT deezer_id FROM ` + table + ` WHERE id = $1`
	return func(w http.ResponseWriter, r *http.Request) {
		var dz string
		if err := s.DB.QueryRow(query, r.PathValue("id")).Scan(&dz); err == nil && isLocalID(dz) {
			writeError(w, http.StatusBadRequest, "own uploads are edited in the upload editor")
			return
		}
		next(w, r)
	}
}

type localResults struct {
	tracks  []deezer.Track
	artists []deezer.Artist
	albums  []deezer.Album
}

func (l localResults) empty() bool {
	return len(l.tracks) == 0 && len(l.artists) == 0 && len(l.albums) == 0
}

func (s *Server) searchLocal(v viewer, q string, limit int) (localResults, error) {
	var out localResults
	words := strings.Fields(q)
	ids := func(query string) ([]string, error) {
		rows, err := s.DB.Query(query, q, limit, pq.Array(words))
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
	albumIDs, err := ids(`SELECT a.deezer_id FROM albums a
		WHERE a.deezer_id LIKE 'k%' AND NOT a.draft AND NOT a.hidden
		  AND EXISTS (SELECT 1 FROM unnest($3::text[]) w WHERE strpos(lower(a.title), lower(w)) > 0)
		  AND (SELECT bool_and(strpos(lower(a.title || ' ' || coalesce(
		         (SELECT string_agg(ar.name, ' ') FROM artists ar WHERE ar.deezer_id = ANY(a.artist_ids)), '')), lower(w)) > 0)
		         FROM unnest($3::text[]) w)
		ORDER BY strpos(lower(a.title), lower($1)) > 0 DESC, a.released_at DESC NULLS LAST LIMIT $2`)
	if err != nil {
		return out, err
	}
	if out.albums, err = s.localAlbumCards(albumIDs); err != nil {
		return out, err
	}
	trackIDs, err := ids(`SELECT t.deezer_id FROM tracks t JOIN albums a ON a.id = t.album_id
		WHERE t.deezer_id LIKE 'k%' AND NOT a.draft AND NOT a.hidden
		  AND EXISTS (SELECT 1 FROM unnest($3::text[]) w WHERE strpos(lower(t.title), lower(w)) > 0)
		  AND (SELECT bool_and(strpos(lower(t.title || ' ' || coalesce(
		         (SELECT string_agg(ar.name, ' ') FROM artists ar
		           WHERE ar.deezer_id = ANY(coalesce(
		             (SELECT o.artist_ids FROM track_overrides o WHERE o.deezer_id = t.deezer_id), a.artist_ids))), '')), lower(w)) > 0)
		         FROM unnest($3::text[]) w)
		ORDER BY strpos(lower(t.title), lower($1)) > 0 DESC, a.released_at DESC NULLS LAST, t.track_no LIMIT $2`)
	if err != nil {
		return out, err
	}
	if out.tracks, err = s.localTracks(v, trackIDs); err != nil {
		return out, err
	}
	out.artists, err = s.searchOwnArtists(v, q, limit, false)
	return out, err
}

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
	s.overlayFor(artistPtrs(artists), nil, nil)
	return artists, nil
}
