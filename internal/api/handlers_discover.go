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

const maxDiscography = 500

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

const maxNotedArtists = 4

func (s *Server) noteReleaseOutsideDiscography(album *deezer.Album) {
	var artists []string
	for _, id := range append([]string{album.ArtistID}, album.MainArtistIDs...) {
		if validDeezerID(id) && !slices.Contains(artists, id) && len(artists) < maxNotedArtists {
			artists = append(artists, id)
		}
	}
	albumID := album.ID
	s.inBackground(func() {
		for _, artistID := range artists {
			list, err := s.artistDiscography(artistID)
			if err != nil || len(list) >= maxDiscography {
				continue
			}
			if slices.ContainsFunc(list, func(a deezer.Album) bool { return a.ID == albumID }) {
				continue
			}
			if _, err := s.DB.Exec(
				`INSERT INTO discography_extras (artist_id, album_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				artistID, albumID); err != nil {
				log.Printf("discography extra %s of %s: %v", albumID, artistID, err)
			}
		}
	})
}

func (s *Server) artistTop(id string) ([]deezer.Track, error) {
	return storedCached(s, "top:"+id, artistTTL, func() ([]deezer.Track, error) {
		return s.Deezer.GetArtistTop(id, topLimit)
	})
}

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
		   AND NOT EXISTS (SELECT 1 FROM tracks t JOIN albums a ON a.id = t.album_id
		                    WHERE t.deezer_id = plays.deezer_id AND a.draft)
		 GROUP BY deezer_id
		 ORDER BY count(*) DESC, max(played_at) DESC
		 LIMIT $2`, id, 2*topLimit, topWindow)
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
	s.overlayFor(nil, nil, out)
	out = creditedTo(id, out)
	return out[:min(len(out), topLimit)], nil
}

func creditedTo(id string, tracks []deezer.Track) []deezer.Track {
	return slices.DeleteFunc(tracks, func(t deezer.Track) bool { return !isCredited(id, t) })
}

func isCredited(artistID string, t deezer.Track) bool {
	return t.ArtistID == artistID || slices.ContainsFunc(t.Contributors, func(c deezer.ArtistRef) bool { return c.ID == artistID })
}

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
	top = cloneTracks(top)
	s.overlayFor(nil, nil, top)
	albums, top = s.creditedDiscography(id, cloneAlbums(albums)), popularTracks(played, creditedTo(id, top))
	s.overlayFor([]*deezer.Artist{&artist}, albumPtrs(albums), nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"artist": artist, "albums": visibleAlbums(r, albums), "top": visibleTracks(r, top),
		"liked": s.likedOf(currentUser(r), id),
	})
}

func (s *Server) handleDiscoverArtistAlbums(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	var albums []deezer.Album
	if !isLocalID(id) {
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
			"album": album, "tracks": tracks, "versions": s.albumVersions(r, album),
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
	s.noteReleaseOutsideDiscography(album)
	if fromApp(r) {
		s.enrichLater(s.Deezer.EnrichContributorsCached(tracks))
		guessContributors(album, tracks)
	} else {
		s.Deezer.EnrichContributors(tracks)
	}
	s.overlayFor(nil, []*deezer.Album{album}, tracks)
	if arranged {
		renumber(tracks, album.ID)
	}
	for i := range tracks {
		tracks[i].Album = album.Title
		tracks[i].CoverURL = album.CoverURL
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": album, "tracks": tracks, "versions": s.albumVersions(r, album)})
}

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

const relatedTTL = 12 * time.Hour

func (s *Server) relatedArtists(id string) ([]deezer.Artist, error) {
	return storedCached(s, "related:"+id, relatedTTL, func() ([]deezer.Artist, error) {
		return s.Deezer.GetArtistRelated(id, 20)
	})
}

func (s *Server) handleDiscoverRelated(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if isLocalID(id) {
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
	artists := make([]deezer.Artist, len(related))
	copy(artists, related)
	s.overlayFor(artistPtrs(artists), nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists})
}

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
