package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"kaikku/internal/deezer"
	"kaikku/internal/ingest"
)

// Админ правит артиста или альбом, которых ещё нет в медиатеке: «claim»
// заводит строку каталога по Deezer ID и отдаёт её id, дальше — обычные
// PUT /api/<...>/{id}. Без треков на главной такая строка не видна.
// Треки правятся по Deezer ID напрямую (ниже).

func (s *Server) handleClaimArtist(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	a, err := s.Deezer.GetArtist(dz)
	if err != nil {
		writeDeezerError(w, "deezer artist lookup failed", err)
		return
	}
	id, err := ingest.EnsureArtist(s.DB, dz, a.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the artist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleClaimAlbum(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	id, err := s.claimAlbum(dz)
	if err != nil {
		writeSaveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// claimAlbum returns the library row of a Deezer album, creating it.
func (s *Server) claimAlbum(dz string) (string, error) {
	var id string
	if s.DB.QueryRow(`SELECT id FROM albums WHERE deezer_id = $1`, dz).Scan(&id) == nil {
		return id, nil
	}
	a, err := s.Deezer.GetAlbum(dz)
	if err != nil {
		return "", err
	}
	artistID, err := ingest.EnsureArtist(s.DB, a.ArtistID, a.ArtistName)
	if err != nil {
		return "", err
	}
	date := sql.NullString{String: a.ReleaseDate, Valid: nullableDate(a.ReleaseDate) != nil}
	recordType := sql.NullString{String: a.RecordType, Valid: a.RecordType != ""}
	return ingest.EnsureAlbum(s.DB, dz, artistID, a.Title, date, recordType)
}

// --- правка трека по Deezer ID ---
//
// Трек правится и без скачивания: если он уже в медиатеке — правится его
// строка, иначе правка хранится в track_edits (миграция 0010) и
// накладывается на ответы Deezer; при скачивании воркер переносит её в
// строку трека.

type deezerTrackEdit struct {
	DeezerID  string `json:"deezer_id"`
	LibraryID string `json:"library_id,omitempty"`
	Title     string `json:"title"`
	TrackNo   *int   `json:"track_no"`
	DiscNo    *int   `json:"disc_no"`
	Edited    bool   `json:"edited"`
	// Missing — трека нет в медиатеке, но его можно загрузить файлом: задача
	// упала (на YouTube не нашлось или не скачалось) или файл удалили.
	Missing bool `json:"missing,omitempty"`
	trackCreditsState
}

// trackMissing — трека нет в медиатеке, а его задача закончилась (упала или
// файл удалили как неслушаемый): файл админа может стать треком.
func (s *Server) trackMissing(dz string) bool {
	var missing bool
	s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM ingest_jobs WHERE source_ref = $1 AND status IN ('failed', 'done'))`,
		"deezer:"+dz).Scan(&missing)
	return missing
}

func (s *Server) libraryTrackByDeezer(dz string) (string, error) {
	var id string
	err := s.DB.QueryRow(`SELECT id FROM tracks WHERE deezer_id = $1`, dz).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Server) handleGetDeezerTrackEdit(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	resp := deezerTrackEdit{DeezerID: dz}
	var trackNo, discNo sql.NullInt64
	err := s.DB.QueryRow(`SELECT id, title, track_no, disc_no, edited FROM tracks WHERE deezer_id = $1`, dz).
		Scan(&resp.LibraryID, &resp.Title, &trackNo, &discNo, &resp.Edited)
	if errors.Is(err, sql.ErrNoRows) {
		t, derr := s.Deezer.GetTrack(dz)
		if derr != nil {
			writeDeezerError(w, "deezer track lookup failed", derr)
			return
		}
		resp.Title = t.Title
		resp.Missing = s.trackMissing(dz)
		trackNo = sql.NullInt64{Int64: int64(t.TrackNo), Valid: t.TrackNo > 0}
		discNo = sql.NullInt64{Int64: int64(t.DiscNo), Valid: t.DiscNo > 0}
		var et string
		var eNo, eDisc sql.NullInt64
		if s.DB.QueryRow(`SELECT title, track_no, disc_no FROM track_edits WHERE deezer_id = $1`, dz).Scan(&et, &eNo, &eDisc) == nil {
			resp.Title, resp.Edited = et, true
			if eNo.Valid {
				trackNo = eNo
			}
			if eDisc.Valid {
				discNo = eDisc
			}
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if trackNo.Valid {
		n := int(trackNo.Int64)
		resp.TrackNo = &n
	}
	if discNo.Valid {
		n := int(discNo.Int64)
		resp.DiscNo = &n
	}
	// Deezer недоступен — без исполнителей и метки: окно правки их тогда не
	// трогает (присылает только изменённое).
	if resp.trackCreditsState, err = s.trackCreditsOf(dz); err != nil {
		log.Printf("track %s: credits: %v", dz, err)
		resp.Artists = []deezer.ArtistRef{}
	}
	resp.Edited = resp.Edited || resp.CreditsEdited || resp.ExplicitEdited
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateDeezerTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	var req trackEdit
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validName(req.Title) {
		writeError(w, http.StatusBadRequest, "title is required (up to 300 characters)")
		return
	}
	libID, err := s.libraryTrackByDeezer(dz)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if libID != "" {
		_, err = s.DB.Exec(
			`UPDATE tracks SET title = $1, track_no = COALESCE($2, track_no), disc_no = COALESCE($3, disc_no), edited = true WHERE id = $4`,
			strings.TrimSpace(req.Title), req.TrackNo, req.DiscNo, libID)
	} else {
		_, err = s.DB.Exec(
			`INSERT INTO track_edits (deezer_id, title, track_no, disc_no) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (deezer_id) DO UPDATE SET title = EXCLUDED.title, track_no = EXCLUDED.track_no,
			        disc_no = EXCLUDED.disc_no, updated_at = now()`,
			dz, strings.TrimSpace(req.Title), req.TrackNo, req.DiscNo)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if err := s.saveTrackCredits(dz, req.Explicit, req.Artists); err != nil {
		writeSaveError(w, err)
		return
	}
	s.handleGetDeezerTrackEdit(w, r)
}

func (s *Server) handleResetDeezerTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM track_edits WHERE deezer_id = $1`, dz); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM track_overrides WHERE deezer_id = $1`, dz); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	libID, err := s.libraryTrackByDeezer(dz)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if libID != "" {
		t, err := s.Deezer.GetTrack(dz)
		if err != nil {
			writeDeezerError(w, "deezer track lookup failed", err)
			return
		}
		if _, err := s.DB.Exec(`UPDATE tracks SET title = $1, track_no = $2, disc_no = $3, edited = false WHERE id = $4`,
			t.Title, nullablePositive(t.TrackNo), nullablePositive(t.DiscNo), libID); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	s.handleGetDeezerTrackEdit(w, r)
}
