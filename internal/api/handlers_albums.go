package api

import (
	"database/sql"
	"log"
	"net/http"

	"kaikku/internal/deezer"
	"kaikku/internal/models"
)

func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var a models.Album
	var credited bool
	err := s.DB.QueryRow(
		`SELECT id, artist_id, title, cover_key, to_char(released_at, 'YYYY-MM-DD'), deezer_id, record_type, edited, cover_custom,
		        hidden, artist_ids IS NOT NULL OR tracklist IS NOT NULL, version_of
		   FROM albums WHERE id = $1`, id,
	).Scan(&a.ID, &a.ArtistID, &a.Title, &a.CoverKey, &a.ReleasedAt, &a.DeezerID, &a.RecordType, &a.Edited, &a.CoverCustom,
		&a.Hidden, &credited, &a.VersionOf)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "album not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// Исполнители и обложка — какими их видят все (Deezer с правкой): окно
	// правки показывает и меняет именно их.
	// Deezer недоступен — без исполнителей: окно правки их тогда не трогает.
	a.Edited = a.Edited || credited
	a.Artists = []deezer.ArtistRef{}
	if a.CoverCustom && a.CoverKey != nil {
		a.ImageURL = "/media/" + *a.CoverKey
	}
	if al, err := s.albumAsSeen(a.DeezerID); err != nil {
		log.Printf("album %s: artists: %v", id, err)
	} else {
		a.Artists = albumRefs(al)
		if a.ImageURL == "" {
			a.ImageURL = al.CoverURL
		}
	}
	writeJSON(w, http.StatusOK, a)
}

// handleDeleteAlbum removes the album, its tracks (FK cascade) and their
// files, including the cover.
func (s *Server) handleDeleteAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	defer tx.Rollback()

	var keys, audio []string
	var coverKey sql.NullString
	err = tx.QueryRow(`SELECT cover_key FROM albums WHERE id = $1 FOR UPDATE`, id).Scan(&coverKey)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "album not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if coverKey.Valid {
		keys = append(keys, coverKey.String)
	}
	rows, err := tx.Query(`DELETE FROM tracks WHERE album_id = $1 RETURNING storage_key`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			audio = append(audio, k)
		}
	}
	rows.Close()

	if _, err := tx.Exec(`DELETE FROM albums WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	// Файл трека может быть общим с тем же треком другого релиза (ISRC).
	s.removeStorageFiles(append(keys, s.unreferencedAudio(audio...)...)...)
	w.WriteHeader(http.StatusNoContent)
}
