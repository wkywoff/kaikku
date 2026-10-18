package api

import (
	"database/sql"
	"log"
	"net/http"

	"kaikku/internal/models"
)

func (s *Server) handleGetArtist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var a models.Artist
	err := s.DB.QueryRow(`SELECT id, name, deezer_id, picture_key, edited, picture_custom FROM artists WHERE id = $1`, id).Scan(&a.ID, &a.Name, &a.DeezerID, &a.PictureKey, &a.Edited, &a.PictureCustom)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// Фото — какое видят все: своё или с Deezer (через прокси, /api/img).
	if a.PictureCustom && a.PictureKey != nil {
		a.ImageURL = "/media/" + *a.PictureKey
	} else if s.Deezer != nil && !isLocalID(a.DeezerID) {
		if dz, err := s.Deezer.GetArtist(a.DeezerID); err != nil {
			log.Printf("artist %s: picture: %v", id, err)
		} else {
			a.ImageURL = dz.PictureURL
		}
	}
	writeJSON(w, http.StatusOK, a)
}
