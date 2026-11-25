package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) playlistTracks(playlistID string) ([]collectionTrack, error) {
	rows, err := s.DB.Query(`
		SELECT i.id, i.meta, i.added_at, u.id, u.username
		  FROM playlist_items i LEFT JOIN users u ON u.id = i.added_by
		 WHERE i.playlist_id = $1 ORDER BY i.position, i.id`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []collectionTrack{}
	for rows.Next() {
		var ct collectionTrack
		var meta []byte
		var byID, byName *string
		if err := rows.Scan(&ct.ItemID, &meta, &ct.AddedAt, &byID, &byName); err != nil {
			return nil, err
		}
		if json.Unmarshal(meta, &ct.Track) != nil {
			continue
		}
		if byID != nil && byName != nil {
			ct.AddedBy = &playlistOwner{ID: *byID, Username: *byName}
		}
		out = append(out, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.overlayCollection(out)
	return out, nil
}

func (s *Server) playlistCollaborators(playlistID string) ([]playlistOwner, error) {
	rows, err := s.DB.Query(`
		SELECT u.id, u.username FROM playlist_collaborators c JOIN users u ON u.id = c.user_id
		 WHERE c.playlist_id = $1 ORDER BY c.added_at`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []playlistOwner{}
	for rows.Next() {
		var u playlistOwner
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Server) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	userID, id, other := currentUser(r), r.PathValue("id"), r.PathValue("user")
	if !s.ownPlaylist(w, r, userID, id) {
		return
	}
	if other == userID {
		writeError(w, http.StatusBadRequest, "the owner is not a collaborator")
		return
	}
	if !s.friends(userID, other) {
		var exists bool
		s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE id::text = $1)`, other).Scan(&exists)
		if !exists {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusForbidden, "only friends (following each other) can be collaborators")
		}
		return
	}
	res, err := s.DB.Exec(
		`INSERT INTO playlist_collaborators (playlist_id, user_id)
		 SELECT $1::uuid, u.id FROM users u WHERE u.id::text = $2
		 ON CONFLICT DO NOTHING`, id, other)
	if err != nil {
		serverError(w, r, "could not add the collaborator", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM playlist_collaborators WHERE playlist_id = $1::uuid AND user_id::text = $2)`, id, other).Scan(&exists)
		if !exists {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
	}
	s.DB.Exec(`UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	userID, id, other := currentUser(r), r.PathValue("id"), r.PathValue("user")
	if other != userID && !s.ownPlaylist(w, r, userID, id) {
		return
	}
	res, err := s.DB.Exec(`DELETE FROM playlist_collaborators WHERE playlist_id::text = $1 AND user_id::text = $2`, id, other)
	if err != nil {
		serverError(w, r, "could not remove the collaborator", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 && other == userID {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
