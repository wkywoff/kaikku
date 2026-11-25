package api

import (
	"database/sql"
	"net/http"

	"github.com/lib/pq"
)

func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var storageKey string
	err := s.DB.QueryRow(`DELETE FROM tracks WHERE id = $1 RETURNING storage_key`, id).Scan(&storageKey)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "track not found")
		return
	} else if err != nil {
		serverError(w, r, "delete failed", err)
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(storageKey)...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unreferencedAudio(keys ...string) []string {
	rows, err := s.DB.Query(`SELECT DISTINCT storage_key FROM tracks WHERE storage_key = ANY($1)`, pq.Array(keys))
	if err != nil {
		return nil
	}
	defer rows.Close()
	used := map[string]bool{}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			used[k] = true
		}
	}
	if rows.Err() != nil {
		return nil
	}
	var free []string
	for _, k := range keys {
		if !used[k] {
			free = append(free, k)
		}
	}
	return free
}
