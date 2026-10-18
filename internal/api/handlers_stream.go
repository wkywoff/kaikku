package api

import (
	"database/sql"
	"net/http"
	"time"
)

func (s *Server) handleStreamURL(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var storageKey, owner string
	var gainDB float64
	var draft bool
	err := s.DB.QueryRow(
		`SELECT t.storage_key, t.gain_db, coalesce(a.draft, false), coalesce(a.owner_id::text, '')
		   FROM tracks t LEFT JOIN albums a ON a.id = t.album_id WHERE t.id = $1`, id).
		Scan(&storageKey, &gainDB, &draft, &owner)
	if err == nil && !viewerOf(r).sees(draft, owner) {
		err = sql.ErrNoRows // чужой черновик (загрузка)
	}
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "track not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	s.touchTrack(id)
	url, expires := s.MediaSigner.URL(storageKey)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        url,
		"expires_at": time.Unix(expires, 0).UTC().Format(time.RFC3339),
		"gain_db":    gainDB, // плеер выравнивает громкость: gain = 10^(gain_db/20)
	})
}
