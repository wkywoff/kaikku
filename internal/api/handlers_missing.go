package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"kaikku/internal/ingest"
	"kaikku/internal/ytdlp"
)

// «Не нашлось» (админ): треки, чьи задачи упали — чаще всего на YouTube не
// нашлось подходящего видео. Админ может указать видео ссылкой, загрузить
// свой файл (PUT /api/tracks/deezer/{id}/audio) или повторить поиск.
// Воркер и сам повторяет упавшее раз в сутки, до трёх попыток
// (ingest.Housekeep).

type missingTrack struct {
	JobID         int64     `json:"job_id"`
	DeezerID      string    `json:"deezer_id"`
	Title         string    `json:"title"`
	Artist        string    `json:"artist"`
	Album         string    `json:"album"`
	AlbumID       string    `json:"album_id,omitempty"`
	CoverURL      string    `json:"cover_url,omitempty"`
	DurationMs    int       `json:"duration_ms"`
	Error         string    `json:"error"`
	Kind          string    `json:"kind"` // not_found | download | audio | other (ingest.FailureKind)
	Attempts      int       `json:"attempts"`
	RequestedBy   string    `json:"requested_by,omitempty"`
	ManualVideoID string    `json:"manual_video_id,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GET /api/admin/missing — упавшие задачи, свежие сверху.
func (s *Server) handleMissing(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`
		SELECT j.id, substr(j.source_ref, 8), j.title, j.artist_name, j.album_title, COALESCE(j.album_deezer_id, ''),
		       COALESCE(j.cover_url, ''), j.duration_ms, COALESCE(j.error, ''), j.attempts, COALESCE(u.username, ''),
		       COALESCE(j.manual_video_id, ''), j.updated_at
		  FROM ingest_jobs j LEFT JOIN users u ON u.id = j.requested_by
		 WHERE j.status = 'failed' AND j.source_ref LIKE 'deezer:%'
		 ORDER BY j.updated_at DESC
		 LIMIT 500`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	out := []missingTrack{}
	for rows.Next() {
		var m missingTrack
		if err := rows.Scan(&m.JobID, &m.DeezerID, &m.Title, &m.Artist, &m.Album, &m.AlbumID, &m.CoverURL, &m.DurationMs,
			&m.Error, &m.Attempts, &m.RequestedBy, &m.ManualVideoID, &m.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		m.Kind = ingest.FailureKind(m.Error)
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": out})
}

// POST /api/admin/missing/{job}/retry — искать заново сейчас, с полным
// запасом попыток.
func (s *Server) handleRetryMissing(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(r.PathValue("job"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	requeued, err := s.requeueJob(jobID, "failed")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !requeued {
		writeError(w, http.StatusConflict, "the job is not failed any more")
		return
	}
	s.DB.Exec(`UPDATE ingest_jobs SET attempts = 0 WHERE id = $1`, jobID)
	var ref string
	if s.DB.QueryRow(`SELECT source_ref FROM ingest_jobs WHERE id = $1`, jobID).Scan(&ref) == nil {
		s.Streams.clearNotFound(ref[len("deezer:"):])
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// PUT /api/tracks/deezer/{id}/video {"url"} — скачать трек с этого видео
// YouTube. Не нашёлся — задача снова в очереди уже без поиска; трек в
// медиатеке (нашлось не то) — его файл заменится.
func (s *Server) handleSetTrackVideo(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	videoID, ok := ytdlp.VideoIDFromURL(req.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "not a YouTube link")
		return
	}
	var status string
	var hasTrack bool
	err := s.DB.QueryRow(
		`SELECT status, EXISTS (SELECT 1 FROM tracks WHERE deezer_id = $2) FROM ingest_jobs WHERE source_ref = $1`, "deezer:"+dz, dz,
	).Scan(&status, &hasTrack)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "the track was never queued")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if status == "pending" || status == "processing" {
		writeError(w, http.StatusConflict, "the track is being downloaded, try again in a minute")
		return
	}
	// replace_audio — у трека уже есть файл: заменить его в той же строке.
	if _, err := s.DB.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, hold_until = NULL,
		        manual_video_id = $2, attempts = 0, replace_audio = $3, updated_at = now()
		  WHERE source_ref = $1 AND status IN ('done', 'failed')`, "deezer:"+dz, videoID, hasTrack,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.Streams.clearNotFound(dz)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued", "video_id": videoID})
}
