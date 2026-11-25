package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"kaikku/internal/ingest"
	"kaikku/internal/ytdlp"
)

const maxCookiesUpload = 1 << 20

var youtubeCheck = ytdlp.Check

func (s *Server) handleUploadYouTubeCookies(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCookiesUpload))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "the file is over 1 MB")
		return
	}
	info, err := ytdlp.SaveCookies(data)
	if err != nil {
		for e, reason := range map[error]string{
			ytdlp.ErrCookiesJSON: "json", ytdlp.ErrCookiesFormat: "format", ytdlp.ErrNoYouTubeCookies: "no_youtube",
		} {
			if errors.Is(err, e) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "reason": reason})
				return
			}
		}
		if errors.Is(err, ytdlp.ErrCookiesOff) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		serverError(w, r, "saving cookies failed", err)
		return
	}
	check, signIn, requeued := s.checkYouTube(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"cookies": info, "check": check, "signin": signIn, "requeued": requeued})
}

func (s *Server) handleDeleteYouTubeCookies(w http.ResponseWriter, r *http.Request) {
	if err := ytdlp.RemoveCookies(); err != nil {
		serverError(w, r, "removing cookies failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const youtubeCheckTimeout = 50 * time.Second

func (s *Server) checkYouTube(ctx context.Context) (check, signIn string, requeued int64) {
	ctx, cancel := context.WithTimeout(ctx, youtubeCheckTimeout)
	defer cancel()
	check = "skipped"
	var videoID string
	if err := s.DB.QueryRowContext(ctx, `
		SELECT v FROM (
		  SELECT COALESCE(video_id, manual_video_id, known_video_id) AS v, 0 AS refused, updated_at
		    FROM ingest_jobs WHERE status = 'failed' AND error LIKE 'blocked:%'
		  UNION ALL
		  SELECT video_id, 1, updated_at FROM ingest_jobs WHERE status = 'done'
		) c WHERE v IS NOT NULL
		ORDER BY refused, updated_at DESC LIMIT 1`).Scan(&videoID); err == nil {
		switch err := youtubeCheck(ctx, videoID); {
		case err == nil:
			check = "ok"
			n, err := ingest.RetryBlocked(s.DB, true)
			if err != nil {
				log.Printf("youtube cookies: queueing refused jobs: %v", err)
			} else if n > 0 {
				log.Printf("youtube cookies: youtube lets the server in, %d refused job(s) queued again", n)
			}
			requeued += n
		case errors.Is(err, ytdlp.ErrBlocked):
			return "blocked", "", 0
		default:
			log.Printf("youtube cookies: checking %s: %v", videoID, err)
			check = "error"
		}
	}

	if err := s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(video_id, manual_video_id, known_video_id) FROM ingest_jobs
		 WHERE status = 'failed' AND error LIKE 'signin:%'
		   AND COALESCE(video_id, manual_video_id, known_video_id) IS NOT NULL
		 ORDER BY updated_at DESC LIMIT 1`).Scan(&videoID); err != nil {
		return check, "", requeued
	}
	switch err := youtubeCheck(ctx, videoID); {
	case err == nil:
		signIn = "ok"
		n, err := ingest.RetrySignIn(s.DB)
		if err != nil {
			log.Printf("youtube cookies: queueing sign-in jobs: %v", err)
		} else if n > 0 {
			log.Printf("youtube cookies: youtube gives %s with them, %d sign-in job(s) queued again", videoID, n)
		}
		requeued += n
	case ytdlp.AgeUnconfirmed(err):
		signIn = "age"
	case errors.Is(err, ytdlp.ErrSignIn):
		signIn = "refused"
	default:
		log.Printf("youtube cookies: checking %s: %v", videoID, err)
		signIn = "error"
	}
	return check, signIn, requeued
}
