package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// Свой треклист релиза (админ, миграция 0020): треки в своём порядке и
// треки с других релизов вместо его собственных — Deezer залил не ту версию
// (цензурную, ремастер, битую), и по ссылке подставляется нужная.
// Подставленный трек — настоящий трек Deezer со своим ISRC, длительностью,
// аудио и текстом; в треклисте он под названием и обложкой этого релиза.

const maxTracklist = 300

// albumTracks returns the release's tracklist with the admin's edit applied;
// arranged — whether there was one (the numbers then follow the new order).
func (s *Server) albumTracks(album *deezer.Album) (tracks []deezer.Track, arranged bool, err error) {
	tracks, err = s.Deezer.GetAlbumTracks(album, 200)
	if err != nil {
		return nil, false, err
	}
	var list, from []string
	if s.DB.QueryRow(`SELECT tracklist, tracklist_from FROM albums WHERE deezer_id = $1 AND tracklist IS NOT NULL`, album.ID).
		Scan(pq.Array(&list), pq.Array(&from)) != nil {
		return tracks, false, nil
	}
	return s.arrangeTracks(tracks, list, from), true, nil
}

// arrangeTracks puts the album's Deezer tracks in the edited order, fetching
// the ones from other releases. Tracks Deezer added after the edit (in
// neither list) go to the end; a track Deezer dropped but the edit keeps is
// fetched on its own.
func (s *Server) arrangeTracks(tracks []deezer.Track, list, from []string) []deezer.Track {
	own := make(map[string]deezer.Track, len(tracks))
	for _, t := range tracks {
		own[t.ID] = t
	}
	out := make([]deezer.Track, 0, len(list))
	for _, id := range list {
		if t, ok := own[id]; ok {
			out = append(out, t)
			continue
		}
		t, err := s.Deezer.GetTrack(id)
		if err != nil {
			log.Printf("tracklist: track %s: %v", id, err)
			continue
		}
		out = append(out, *t)
	}
	for _, t := range tracks {
		if !slices.Contains(list, t.ID) && !slices.Contains(from, t.ID) {
			out = append(out, t)
		}
	}
	return out
}

// renumber numbers an arranged tracklist by position within each disc. A
// track from another release joins the disc of the track before it.
func renumber(tracks []deezer.Track, albumID string) {
	next := map[int]int{}
	disc := 1
	for i := range tracks {
		t := &tracks[i]
		if t.AlbumID == albumID && t.DiscNo > 0 {
			disc = t.DiscNo
		}
		t.DiscNo = disc
		next[disc]++
		t.TrackNo = next[disc]
	}
}

// saveTracklist stores the release's tracklist (Deezer track IDs in order);
// the same as Deezer's clears the edit.
func (s *Server) saveTracklist(albumDeezerID string, ids []string) error {
	if len(ids) == 0 || len(ids) > maxTracklist {
		return &badRequest{fmt.Sprintf("tracklist must have 1 to %d tracks", maxTracklist)}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validDeezerID(id) || seen[id] {
			return &badRequest{"tracklist must be distinct numeric Deezer track IDs"}
		}
		seen[id] = true
	}
	album, err := s.Deezer.GetAlbum(albumDeezerID)
	if err != nil {
		return err
	}
	tracks, err := s.Deezer.GetAlbumTracks(album, 200)
	if err != nil {
		return err
	}
	from := make([]string, len(tracks))
	for i, t := range tracks {
		from[i] = t.ID
	}
	for _, id := range ids {
		if slices.Contains(from, id) {
			continue
		}
		if _, err := s.Deezer.GetTrack(id); err != nil {
			return &badRequest{fmt.Sprintf("track %s not found on Deezer", id)}
		}
	}
	if slices.Equal(ids, from) {
		_, err = s.DB.Exec(`UPDATE albums SET tracklist = NULL, tracklist_from = NULL WHERE deezer_id = $1`, albumDeezerID)
	} else {
		_, err = s.DB.Exec(`UPDATE albums SET tracklist = $2, tracklist_from = $3 WHERE deezer_id = $1`,
			albumDeezerID, pq.Array(ids), pq.Array(from))
	}
	return err
}

// handleReplaceAlbumTrack — «заменить трек по ссылке» из окна правки трека:
// в треклисте релиза трек track встаёт на место with. Ответ — треклист
// релиза, как в /api/discover/albums/{id}.
func (s *Server) handleReplaceAlbumTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	var req struct {
		Track string `json:"track"`
		With  string `json:"with"`
	}
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validDeezerID(req.Track) || !validDeezerID(req.With) {
		writeError(w, http.StatusBadRequest, "track and with must be Deezer track IDs")
		return
	}
	if _, err := s.claimAlbum(dz); err != nil {
		writeSaveError(w, err)
		return
	}
	album, err := s.Deezer.GetAlbum(dz)
	if err != nil {
		writeDeezerError(w, "deezer album lookup failed", err)
		return
	}
	tracks, _, err := s.albumTracks(album)
	if err != nil {
		writeDeezerError(w, "deezer album tracks lookup failed", err)
		return
	}
	ids := make([]string, len(tracks))
	at := -1
	for i, t := range tracks {
		ids[i] = t.ID
		if t.ID == req.Track {
			at = i
		}
	}
	if at < 0 {
		writeError(w, http.StatusBadRequest, "the track is not on this release")
		return
	}
	if req.With != req.Track && slices.Contains(ids, req.With) {
		writeError(w, http.StatusBadRequest, "this track is already on the release")
		return
	}
	ids[at] = req.With
	if err := s.saveTracklist(dz, ids); err != nil {
		writeSaveError(w, err)
		return
	}
	s.handleDiscoverAlbum(w, r)
}
