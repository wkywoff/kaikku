package api

import (
	"database/sql"
	"net/http"

	"github.com/lib/pq"
)

// handleDeleteTrack removes the track and its audio file (unless another
// release of the same recording shares it). The ingest job that produced it
// keeps its row (result_track_id becomes NULL), so adding the track again
// re-queues it.
func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var storageKey string
	err := s.DB.QueryRow(`DELETE FROM tracks WHERE id = $1 RETURNING storage_key`, id).Scan(&storageKey)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "track not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(storageKey)...)
	w.WriteHeader(http.StatusNoContent)
}

// unreferencedAudio drops the audio files still used by other tracks: one
// recording on several releases (same ISRC) shares one file (worker.go).
func (s *Server) unreferencedAudio(keys ...string) []string {
	rows, err := s.DB.Query(`SELECT DISTINCT storage_key FROM tracks WHERE storage_key = ANY($1)`, pq.Array(keys))
	if err != nil {
		return nil // не знаем наверняка — лучше оставить файл, чем удалить чужой
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
