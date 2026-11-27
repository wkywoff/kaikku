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
		serverError(w, r, "could not create the artist", err)
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
		writeSaveError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

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

type deezerTrackEdit struct {
	DeezerID  string `json:"deezer_id"`
	LibraryID string `json:"library_id,omitempty"`
	Title     string `json:"title"`
	TrackNo   *int   `json:"track_no"`
	DiscNo    *int   `json:"disc_no"`
	Edited    bool   `json:"edited"`
	Missing   bool   `json:"missing,omitempty"`
	trackCreditsState
}

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
		serverError(w, r, "query failed", err)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.validTitle() {
		writeError(w, http.StatusBadRequest, "title is required (up to 300 characters)")
		return
	}
	if req.Title != nil {
		if err := s.saveTrackTitle(dz, strings.TrimSpace(*req.Title), req.TrackNo, req.DiscNo); err != nil {
			serverError(w, r, "update failed", err)
			return
		}
	}
	if err := s.saveTrackCredits(dz, req.Explicit, req.Artists); err != nil {
		writeSaveError(w, r, err)
		return
	}
	s.handleGetDeezerTrackEdit(w, r)
}

func (s *Server) saveTrackTitle(dz, title string, trackNo, discNo *int) error {
	libID, err := s.libraryTrackByDeezer(dz)
	if err != nil {
		return err
	}
	if libID != "" {
		_, err = s.DB.Exec(
			`UPDATE tracks SET title = $1, track_no = COALESCE($2, track_no), disc_no = COALESCE($3, disc_no), edited = true WHERE id = $4`,
			title, trackNo, discNo, libID)
		return err
	}
	_, err = s.DB.Exec(
		`INSERT INTO track_edits (deezer_id, title, track_no, disc_no) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (deezer_id) DO UPDATE SET title = EXCLUDED.title, track_no = EXCLUDED.track_no,
		        disc_no = EXCLUDED.disc_no, updated_at = now()`,
		dz, title, trackNo, discNo)
	return err
}

func (s *Server) handleResetDeezerTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM track_edits WHERE deezer_id = $1`, dz); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM track_overrides WHERE deezer_id = $1`, dz); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	libID, err := s.libraryTrackByDeezer(dz)
	if err != nil {
		serverError(w, r, "query failed", err)
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
			serverError(w, r, "update failed", err)
			return
		}
	}
	s.handleGetDeezerTrackEdit(w, r)
}
