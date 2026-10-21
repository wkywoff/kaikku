package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	_ "image/jpeg" // DecodeConfig для проверки загруженных картинок
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kaikku/internal/deezer"
)

// Ручные правки админа. Страницы артиста/альбома и поиск строятся из
// ответов Deezer, поэтому правки (хранятся в наших artists/albums/tracks,
// флаги edited / *_custom — миграция 0006) накладываются поверх этих
// ответов: overlay ниже. Артиста и альбом правят по нашему id (заводится
// «claim», handlers_claim.go), трек — по Deezer ID (там же).

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

// --- правка полей ---

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
	if !s.affected(w, res, err, "artist") {
		return
	}
	s.handleGetArtist(w, r)
}

type albumEdit struct {
	Title      string   `json:"title"`
	ReleasedAt string   `json:"released_at"` // YYYY-MM-DD или "" — без даты
	RecordType string   `json:"record_type"` // album | ep | single | compile
	Artists    []string `json:"artists"`     // Deezer ID исполнителей, основной первый; нет — не менять
	Hidden     *bool    `json:"hidden"`
	Tracklist  []string `json:"tracklist"`  // Deezer ID треков по порядку (tracklist.go); нет — не менять
	VersionOf  *string  `json:"version_of"` // ID основного релиза (versions.go), "" — отвязать; нет — не менять
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
	if !s.affected(w, res, err, "album") {
		return
	}
	if req.Artists != nil {
		if err := s.saveAlbumCredits(r.PathValue("id"), req.Artists); err != nil {
			writeSaveError(w, err)
			return
		}
	}
	if req.Hidden != nil {
		if _, err := s.DB.Exec(`UPDATE albums SET hidden = $2 WHERE id = $1`, r.PathValue("id"), *req.Hidden); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.VersionOf != nil {
		if err := s.saveVersionOf(viewerOf(r), r.PathValue("id"), *req.VersionOf); err != nil {
			writeSaveError(w, err)
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
			writeSaveError(w, err)
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
	Artists  []string `json:"artists,omitempty"` // Deezer ID исполнителей, основной первый; нет — не менять
}

// --- сброс к данным Deezer ---

// handleResetArtist drops manual edits of the name and picture.
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
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	a, err := s.Deezer.GetArtist(deezerID)
	if err != nil {
		writeDeezerError(w, "deezer artist lookup failed", err)
		return
	}
	// Своё фото уходит; картинку Deezer фронт берёт через прокси (/api/img).
	newPicture := picture
	if custom {
		newPicture = sql.NullString{}
	}
	if _, err := s.DB.Exec(
		`UPDATE artists SET name = $1, edited = false, picture_key = $2, picture_custom = false WHERE id = $3`,
		a.Name, newPicture, id,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if custom && picture.Valid {
		s.removeStorageFiles(picture.String)
	}
	s.handleGetArtist(w, r)
}

// handleResetAlbum drops manual edits of title/date/type, the cover, the
// credits and the tracklist.
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
		writeError(w, http.StatusInternalServerError, "query failed")
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
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if custom && cover.Valid {
		s.removeStorageFiles(cover.String)
	}
	s.handleGetAlbum(w, r)
}

// --- свои картинки ---

// Картинка приходит уже уменьшенной и в JPEG (фронт пережимает её на
// canvas до 1000 px), так что лимит совпадает с client_max_body_size в nginx.
// Стороны ограничены: маленький файл может объявить огромную картинку
// («JPEG-бомба»), и распаковка при уменьшении (covers.go) съела бы память —
// 4096×4096 это ~90 МБ, а обложку плейлиста загружает любой пользователь.
// 2048 с запасом: фронт шлёт не больше 1000.
const (
	maxImageUpload = 1 << 20
	maxImageSide   = 2048
)

// saveUploadedImage validates a JPEG body and stores it under covers/custom.
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
		writeError(w, http.StatusInternalServerError, "could not save image")
		return "", false
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save image")
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
			writeError(w, http.StatusInternalServerError, "update failed")
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
			writeError(w, http.StatusInternalServerError, "update failed")
		}
		return
	}
	if old.Valid {
		s.removeStorageFiles(old.String)
	}
	s.handleGetAlbum(w, r)
}

// --- мелочи ---

func validName(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && len([]rune(s)) <= 300
}

func (s *Server) affected(w http.ResponseWriter, res sql.Result, err error, what string) bool {
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, what+" not found")
		return false
	}
	return true
}
