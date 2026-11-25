package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kaikku/internal/deezer"
)

func albumPtrs(albums []deezer.Album) []*deezer.Album {
	out := make([]*deezer.Album, len(albums))
	for i := range albums {
		out[i] = &albums[i]
	}
	return out
}

func artistPtrs(artists []deezer.Artist) []*deezer.Artist {
	out := make([]*deezer.Artist, len(artists))
	for i := range artists {
		out[i] = &artists[i]
	}
	return out
}

type artistEdit struct {
	Name string `json:"name"`
}

func (s *Server) handleUpdateArtist(w http.ResponseWriter, r *http.Request) {
	var req artistEdit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validName(req.Name) {
		writeError(w, http.StatusBadRequest, "name is required (up to 300 characters)")
		return
	}
	res, err := s.DB.Exec(`UPDATE artists SET name = $1, edited = true WHERE id = $2`, strings.TrimSpace(req.Name), r.PathValue("id"))
	if !s.affected(w, r, res, err, "artist") {
		return
	}
	s.handleGetArtist(w, r)
}

type albumEdit struct {
	Title      string   `json:"title"`
	ReleasedAt string   `json:"released_at"`
	RecordType string   `json:"record_type"`
	Artists    []string `json:"artists"`
	Hidden     *bool    `json:"hidden"`
	Tracklist  []string `json:"tracklist"`
	VersionOf  *string  `json:"version_of"`
}

var recordTypes = map[string]bool{"album": true, "ep": true, "single": true, "compile": true}

func (s *Server) handleUpdateAlbum(w http.ResponseWriter, r *http.Request) {
	var req albumEdit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validName(req.Title) {
		writeError(w, http.StatusBadRequest, "title is required (up to 300 characters)")
		return
	}
	if req.RecordType != "" && !recordTypes[req.RecordType] {
		writeError(w, http.StatusBadRequest, "record_type must be album, ep, single or compile")
		return
	}
	var released any
	if req.ReleasedAt != "" {
		if _, err := time.Parse("2006-01-02", req.ReleasedAt); err != nil {
			writeError(w, http.StatusBadRequest, "released_at must be YYYY-MM-DD")
			return
		}
		released = req.ReleasedAt
	}
	res, err := s.DB.Exec(
		`UPDATE albums SET title = $1, released_at = $2, record_type = COALESCE(NULLIF($3, ''), record_type), edited = true WHERE id = $4`,
		strings.TrimSpace(req.Title), released, req.RecordType, r.PathValue("id"),
	)
	if !s.affected(w, r, res, err, "album") {
		return
	}
	if req.Artists != nil {
		if err := s.saveAlbumCredits(r.PathValue("id"), req.Artists); err != nil {
			writeSaveError(w, r, err)
			return
		}
	}
	if req.Hidden != nil {
		if _, err := s.DB.Exec(`UPDATE albums SET hidden = $2 WHERE id = $1`, r.PathValue("id"), *req.Hidden); err != nil {
			serverError(w, r, "update failed", err)
			return
		}
	}
	if req.VersionOf != nil {
		if err := s.saveVersionOf(viewerOf(r), r.PathValue("id"), *req.VersionOf); err != nil {
			writeSaveError(w, r, err)
			return
		}
	}
	if req.Tracklist != nil {
		var dz string
		err := s.DB.QueryRow(`SELECT deezer_id FROM albums WHERE id = $1`, r.PathValue("id")).Scan(&dz)
		if err == nil {
			err = s.saveTracklist(dz, req.Tracklist)
		}
		if err != nil {
			writeSaveError(w, r, err)
			return
		}
	}
	s.handleGetAlbum(w, r)
}

type trackEdit struct {
	Title    string   `json:"title"`
	TrackNo  *int     `json:"track_no,omitempty"`
	DiscNo   *int     `json:"disc_no,omitempty"`
	Explicit *bool    `json:"explicit,omitempty"`
	Artists  []string `json:"artists,omitempty"`
}

func (s *Server) handleResetArtist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var deezerID string
	var picture sql.NullString
	var custom bool
	err := s.DB.QueryRow(`SELECT deezer_id, picture_key, picture_custom FROM artists WHERE id = $1`, id).Scan(&deezerID, &picture, &custom)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	} else if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	a, err := s.Deezer.GetArtist(deezerID)
	if err != nil {
		writeDeezerError(w, "deezer artist lookup failed", err)
		return
	}
	newPicture := picture
	if custom {
		newPicture = sql.NullString{}
	}
	if _, err := s.DB.Exec(
		`UPDATE artists SET name = $1, edited = false, picture_key = $2, picture_custom = false WHERE id = $3`,
		a.Name, newPicture, id,
	); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	if custom && picture.Valid {
		s.removeStorageFiles(picture.String)
	}
	s.handleGetArtist(w, r)
}

func (s *Server) handleResetAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var deezerID string
	var cover sql.NullString
	var custom bool
	err := s.DB.QueryRow(`SELECT deezer_id, cover_key, cover_custom FROM albums WHERE id = $1`, id).Scan(&deezerID, &cover, &custom)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "album not found")
		return
	} else if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	a, err := s.Deezer.GetAlbum(deezerID)
	if err != nil {
		writeDeezerError(w, "deezer album lookup failed", err)
		return
	}
	newCover := cover
	if custom {
		newCover = sql.NullString{}
	}
	if _, err := s.DB.Exec(
		`UPDATE albums SET title = $1, released_at = $2, record_type = NULLIF($3, ''), edited = false,
		                   cover_key = $4, cover_custom = false, artist_ids = NULL, credits_from = NULL,
		                   tracklist = NULL, tracklist_from = NULL
		  WHERE id = $5`,
		a.Title, nullableDate(a.ReleaseDate), a.RecordType, newCover, id,
	); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	if custom && cover.Valid {
		s.removeStorageFiles(cover.String)
	}
	s.handleGetAlbum(w, r)
}

const (
	maxImageUpload = 1 << 20
	maxImageSide   = 2048
)

func (s *Server) saveUploadedImage(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.StorageDir == "" {
		writeError(w, http.StatusServiceUnavailable, "storage is not configured")
		return "", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageUpload))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "image must be at most 1 MB")
		return "", false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || format != "jpeg" || cfg.Width < 64 || cfg.Height < 64 || cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		writeError(w, http.StatusBadRequest, "image must be a JPEG from 64×64 to 2048×2048")
		return "", false
	}
	key := filepath.Join("covers", "custom", randomKey()+".jpg")
	path := filepath.Join(s.StorageDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		serverError(w, r, "could not save image", err)
		return "", false
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		serverError(w, r, "could not save image", err)
		return "", false
	}
	return key, true
}

func (s *Server) handleUploadArtistPicture(w http.ResponseWriter, r *http.Request) {
	key, ok := s.saveUploadedImage(w, r)
	if !ok {
		return
	}
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE artists a SET picture_key = $1, picture_custom = true
		   FROM (SELECT picture_key FROM artists WHERE id = $2) prev
		  WHERE a.id = $2 RETURNING prev.picture_key`, key, r.PathValue("id"),
	).Scan(&old)
	if err != nil {
		s.removeStorageFiles(key)
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "artist not found")
		} else {
			serverError(w, r, "update failed", err)
		}
		return
	}
	if old.Valid {
		s.removeStorageFiles(old.String)
	}
	s.handleGetArtist(w, r)
}

func (s *Server) handleUploadAlbumCover(w http.ResponseWriter, r *http.Request) {
	key, ok := s.saveUploadedImage(w, r)
	if !ok {
		return
	}
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE albums a SET cover_key = $1, cover_custom = true
		   FROM (SELECT cover_key FROM albums WHERE id = $2) prev
		  WHERE a.id = $2 RETURNING prev.cover_key`, key, r.PathValue("id"),
	).Scan(&old)
	if err != nil {
		s.removeStorageFiles(key)
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "album not found")
		} else {
			serverError(w, r, "update failed", err)
		}
		return
	}
	if old.Valid {
		s.removeStorageFiles(old.String)
	}
	s.handleGetAlbum(w, r)
}

func (s *Server) handleDeleteAlbumCover(w http.ResponseWriter, r *http.Request) {
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE albums a SET cover_key = NULL, cover_custom = false
		   FROM (SELECT cover_key, cover_custom FROM albums WHERE id = $1 FOR UPDATE) prev
		  WHERE a.id = $1 AND prev.cover_custom RETURNING prev.cover_key`, r.PathValue("id"),
	).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		serverError(w, r, "update failed", err)
		return
	}
	if old.Valid {
		s.removeStorageFiles(old.String)
	}
	s.handleGetAlbum(w, r)
}

func validName(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && len([]rune(s)) <= 300
}

func (s *Server) affected(w http.ResponseWriter, r *http.Request, res sql.Result, err error, what string) bool {
	if err != nil {
		serverError(w, r, "update failed", err)
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, what+" not found")
		return false
	}
	return true
}
