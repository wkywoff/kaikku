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

// Куки YouTube (админ, страница «Не нашлось»): если YouTube не пускает
// сервер («Sign in to confirm you're not a bot»), а прокси нет, обычно
// помогает cookies.txt от аккаунта YouTube. Сами куки сервер не отдаёт
// никогда — только сколько их и есть ли среди них вход (ytdlp.CookiesInfo).

const maxCookiesUpload = 1 << 20

// youtubeCheck — проверка, пускает ли YouTube (подменяют тесты).
var youtubeCheck = ytdlp.Check

// PUT /api/admin/youtube/cookies (тело — cookies.txt) — сохранить и сразу
// проверить. Ответ: {cookies, check}; check — ok (YouTube пускает, отказанные
// задачи снова в очереди), blocked (всё ещё нет), error или skipped (не на
// чем проверить).
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
	writeJSON(w, http.StatusOK, map[string]any{"cookies": info, "check": s.checkYouTube(r.Context())})
}

// DELETE /api/admin/youtube/cookies
func (s *Server) handleDeleteYouTubeCookies(w http.ResponseWriter, r *http.Request) {
	if err := ytdlp.RemoveCookies(); err != nil {
		serverError(w, r, "removing cookies failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// youtubeCheckTimeout — меньше минуты, которую ждёт nginx.
const youtubeCheckTimeout = 45 * time.Second

// checkYouTube asks YouTube, the way the worker does, for a video it
// refused lately, or else for one that downloaded before. When YouTube lets
// the server in, the refused jobs go back to the queue at once.
func (s *Server) checkYouTube(ctx context.Context) string {
	var videoID string
	if err := s.DB.QueryRowContext(ctx, `
		SELECT v FROM (
		  SELECT COALESCE(video_id, manual_video_id, known_video_id) AS v, 0 AS refused, updated_at
		    FROM ingest_jobs WHERE status = 'failed' AND error LIKE 'blocked:%'
		  UNION ALL
		  SELECT video_id, 1, updated_at FROM ingest_jobs WHERE status = 'done'
		) c WHERE v IS NOT NULL
		ORDER BY refused, updated_at DESC LIMIT 1`).Scan(&videoID); err != nil {
		return "skipped"
	}
	ctx, cancel := context.WithTimeout(ctx, youtubeCheckTimeout)
	defer cancel()
	switch err := youtubeCheck(ctx, videoID); {
	case err == nil:
		if n, err := ingest.RetryBlocked(s.DB, true); err != nil {
			log.Printf("youtube cookies: queueing refused jobs: %v", err)
		} else if n > 0 {
			log.Printf("youtube cookies: youtube lets the server in, %d refused job(s) queued again", n)
		}
		return "ok"
	case errors.Is(err, ytdlp.ErrBlocked):
		return "blocked"
	default:
		log.Printf("youtube cookies: checking %s: %v", videoID, err)
		return "error"
	}
}
