package api

// Воспроизведение трека, которого ещё нет в медиатеке, во время скачивания.
//
//	GET /api/stream/deezer/{id}/url  (Bearer) — ставит трек на скачивание
//	    (ingest-job, как лайк), сразу начинает качать сам и отдаёт подписанную ссылку
//	    на поток; если трек уже в медиатеке — обычную ссылку /media, как
//	    /api/tracks/{id}/stream-url.
//	GET /api/stream/deezer/{id}?st=…&e=…  — сам поток: <audio> не умеет
//	    слать Bearer, поэтому подпись как у /media (media.Signer).
//
// Поток — формат 140 с YouTube (AAC в DASH-MP4) как есть, без
// перекодирования (ytdlp.OpenStream: поиск и извлечение «тёплым» yt-dlp,
// затем прямое скачивание; замер на сервере — звук через ~2–3 с после
// клика). Он пишется в файл, а запросы читают этот растущий файл:
// сколько бы запросов (перемотка, повтор) ни пришло, трек качается один раз.
// Полный размер известен по первым ~1 КБ (sidx, media.DashFileSize), так что
// ответ сразу с Content-Length и Range, как у статики, — без этого iOS
// Safari не играет <audio>.
//
// Скачанный файл отдаётся воркеру (ingest.PrefetchPath), и тот не качает
// трек заново. Чтобы воркер не взял job раньше, ingest_jobs.hold_until.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/ingest"
	"kaikku/internal/media"
	"kaikku/internal/ytdlp"
)

// StreamSource finds the recording on YouTube and opens its audio
// (ytdlp.OpenStream in production; a fake in tests).
type StreamSource func(ctx context.Context, want ytdlp.Want) (videoID string, body io.ReadCloser, err error)

// Матчинг — с теми же параметрами, что у воркера (internal/ingest/worker.go).
const (
	streamSearchCandidates  = 15
	streamDurationTolerance = 4000
	streamMaxTries          = 3
)

func defaultStreamSource(ctx context.Context, want ytdlp.Want) (string, io.ReadCloser, error) {
	return ytdlp.OpenStream(ctx, want, streamSearchCandidates, streamDurationTolerance, streamMaxTries)
}

const (
	// Верхняя граница на поиск + скачивание; столько же job придержан на время скачивания.
	streamDownloadTimeout = 3 * time.Minute
	// Сколько скачанный файл ещё отдаётся по той же ссылке (перемотка, пауза):
	// не меньше срока жизни подписанной ссылки.
	streamRetain = 15 * time.Minute
	// Если в первых байтах нет sidx, размер не узнать до конца скачивания.
	streamHeaderLimit = 64 << 10
	// Одновременных скачиваний: 1 vCPU, и воркер параллельно качает до трёх.
	streamMaxDownloads = 3
)

// StreamHub keeps the downloads in progress (and recently finished), one
// per Deezer track.
type StreamHub struct {
	Dir        string // рабочий каталог API, очищается при старте
	StagingDir string // STAGING_DIR воркера: сюда — готовый файл (hard link)
	Source     StreamSource

	mu     sync.Mutex
	active map[string]*streamDownload
	slots  chan struct{}
	// notFound — когда для трека не нашлось видео на YouTube: следующие
	// запросы notFoundTTL отвечают сразу, без нового поиска на 5–10 с.
	notFound map[string]time.Time
}

const notFoundTTL = 10 * time.Minute

func (h *StreamHub) markNotFound(deezerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.notFound == nil {
		h.notFound = map[string]time.Time{}
	}
	now := time.Now()
	for id, at := range h.notFound {
		if now.Sub(at) > notFoundTTL {
			delete(h.notFound, id)
		}
	}
	h.notFound[deezerID] = now
}

// recentlyNotFound — недавно для трека не нашлось видео.
func (h *StreamHub) recentlyNotFound(deezerID string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	at, ok := h.notFound[deezerID]
	return ok && time.Since(at) < notFoundTTL
}

// clearNotFound — админ указал видео или загрузил файл.
func (h *StreamHub) clearNotFound(deezerID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.notFound, deezerID)
}

// NewStreamHub prepares dir (wiping leftovers of a previous run — only the
// API writes there). stagingDir must be on the same filesystem for the
// hand-off to be a hard link; otherwise the file is copied.
func NewStreamHub(dir, stagingDir string) (*StreamHub, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &StreamHub{Dir: dir, StagingDir: stagingDir, Source: defaultStreamSource}, nil
}

type streamDownload struct {
	path string

	mu      sync.Mutex
	changed chan struct{} // закрывается и заменяется при каждом изменении
	written int64
	size    int64 // -1, пока неизвестен
	done    bool
	err     error
}

type streamState struct {
	written, size int64
	done          bool
	err           error
	changed       <-chan struct{}
}

func (d *streamDownload) state() streamState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return streamState{d.written, d.size, d.done, d.err, d.changed}
}

func (d *streamDownload) update(f func()) {
	d.mu.Lock()
	f()
	close(d.changed)
	d.changed = make(chan struct{})
	d.mu.Unlock()
}

// wait blocks until cond holds for the download's state (or ctx ends).
func (d *streamDownload) wait(ctx context.Context, cond func(streamState) bool) (streamState, error) {
	for {
		st := d.state()
		if cond(st) {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-st.changed:
		}
	}
}

// streamJob is what the download needs from ingest_jobs.
type streamJob struct {
	id   int64
	want ytdlp.Want
}

// get returns the download for deezerID, starting it if there is none.
func (h *StreamHub) get(db *sql.DB, deezerID string, job streamJob) *streamDownload {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		h.active = map[string]*streamDownload{}
		h.slots = make(chan struct{}, streamMaxDownloads)
	}
	if d := h.active[deezerID]; d != nil {
		return d
	}
	d := &streamDownload{
		path:    filepath.Join(h.Dir, fmt.Sprintf("%s-%d.m4a", deezerID, time.Now().UnixNano())),
		changed: make(chan struct{}),
		size:    -1,
	}
	h.active[deezerID] = d
	go h.run(db, deezerID, job, d)
	return d
}

// lookup returns a download that is running or finished, or nil.
func (h *StreamHub) lookup(deezerID string) *streamDownload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active[deezerID]
}

func (h *StreamHub) forget(deezerID string, d *streamDownload) {
	h.mu.Lock()
	if h.active[deezerID] == d {
		delete(h.active, deezerID)
	}
	h.mu.Unlock()
	os.Remove(d.path)
}

// run downloads the track into d.path. It isn't tied to any request: if the
// listener goes away mid-download, the file still reaches the worker.
func (h *StreamHub) run(db *sql.DB, deezerID string, job streamJob, d *streamDownload) {
	ctx, cancel := context.WithTimeout(context.Background(), streamDownloadTimeout)
	defer cancel()

	started := time.Now()
	var audioAfter time.Duration
	var videoID string
	// Паника в скачивании — как ошибка: иначе ждущие поток висели бы, а
	// задача так и осталась бы придержанной (hold_until).
	err := guard("stream download", func() (err error) {
		videoID, err = h.download(ctx, job, d, func() { audioAfter = time.Since(started) })
		return err
	})

	if err != nil {
		log.Printf("stream deezer:%s: %v", deezerID, err)
		if errors.Is(err, ytdlp.ErrNoMatch) {
			h.markNotFound(deezerID)
		}
		d.update(func() { d.err = err })
		h.forget(deezerID, d) // следующий запрос начнёт заново
	} else {
		log.Printf("stream deezer:%s: video %s, audio after %.1fs, downloaded after %.1fs (hand-off: %v)",
			deezerID, videoID, audioAfter.Seconds(), time.Since(started).Seconds(), job.id != 0)
		time.AfterFunc(streamRetain, func() { h.forget(deezerID, d) })
	}
	if job.id == 0 {
		return
	}
	// Отдаём воркеру. Файл должен лежать на месте до снятия hold.
	if err == nil && h.StagingDir != "" {
		if linkErr := linkOrCopy(d.path, ingest.PrefetchPath(h.StagingDir, job.id)); linkErr != nil {
			log.Printf("stream deezer:%s: hand-off to worker: %v", deezerID, linkErr)
		} else if _, dbErr := db.Exec(`UPDATE ingest_jobs SET video_id = $2 WHERE id = $1`, job.id, videoID); dbErr != nil {
			log.Printf("stream deezer:%s: saving video_id: %v", deezerID, dbErr)
		}
	}
	if _, dbErr := db.Exec(`UPDATE ingest_jobs SET hold_until = NULL WHERE id = $1`, job.id); dbErr != nil {
		log.Printf("stream deezer:%s: releasing job: %v", deezerID, dbErr)
	}
}

func (h *StreamHub) download(ctx context.Context, job streamJob, d *streamDownload, onAudio func()) (string, error) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for a download slot: %w", ctx.Err())
	}

	videoID, body, err := h.Source(ctx, job.want)
	if err != nil {
		return "", err
	}
	defer body.Close()
	onAudio()

	f, err := os.Create(d.path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 0, 4096)
	sizeKnown, sizeHopeless := false, false
	buf := make([]byte, 32<<10)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			size := int64(-1)
			if !sizeKnown && !sizeHopeless {
				head = append(head, buf[:min(n, streamHeaderLimit-len(head))]...)
				if s, ok, needMore := media.DashFileSize(head); ok {
					size, sizeKnown = s, true
				} else if !needMore || len(head) >= streamHeaderLimit {
					sizeHopeless = true
				}
			}
			d.update(func() {
				d.written += int64(n)
				if size >= 0 {
					d.size = size
				}
			})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	st := d.state()
	if st.size >= 0 && st.written != st.size {
		return "", fmt.Errorf("downloaded %d bytes, expected %d", st.written, st.size)
	}
	d.update(func() {
		d.done = true
		d.size = d.written
	})
	return videoID, nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("copying: %v %v", copyErr, closeErr)
	}
	return os.Rename(tmp, dst)
}

func streamPath(deezerID string) string { return "/api/stream/deezer/" + deezerID }

// handleStreamURL (GET /api/stream/deezer/{id}/url) — see the top of the file.
//
//	в медиатеке: {"status":"duplicate","job_id"?,"track_id","url","expires_at","gain_db"}
//	нет:         {"status":"queued|duplicate","job_id","job_status"?,"url","expires_at","gain_db":0}
//
// status/job_id/job_status — состояние ingest-job'а (как у /api/ingest/lookup);
// track_id — трек медиатеки, когда он уже скачан.
func (s *Server) handleDeezerStreamURL(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	deezerID := r.PathValue("id")
	if isLocalID(deezerID) {
		s.ownStreamURL(w, r, deezerID)
		return
	}
	if !validDeezerID(deezerID) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if s.Streams == nil {
		writeError(w, http.StatusServiceUnavailable, "streaming is not configured")
		return
	}

	if resp, err := s.libraryStreamURL(deezerID); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	} else if resp != nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// Только что не нашлось на YouTube — так и говорим, плеер покажет почему
	// пропускает трек (а не ищет заново ещё 5–10 с).
	if s.Streams.recentlyNotFound(deezerID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the song was not found on YouTube", "reason": "not_found"})
		return
	}

	result, err := s.enqueueTrack(userID, deezerID)
	if err != nil {
		writeDeezerError(w, "ingest failed", err)
		return
	}
	// Та же запись на другом релизе (ISRC) уже в медиатеке — играет её файл,
	// без YouTube; воркер сразу заведёт на него и этот трек (worker.go).
	if twin, err := s.sameRecordingURL(deezerID); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	} else if twin != nil {
		for k, v := range twin {
			result[k] = v
		}
		writeJSON(w, statusFor(result), result)
		return
	}
	// Скачивание — сразу, не дожидаясь, пока браузер запросит поток: так
	// звук на круг клиент↔сервер раньше.
	if err := s.startStream(deezerID); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	url, expires := s.MediaSigner.PathURL(streamPath(deezerID))
	result["url"] = url
	result["expires_at"] = time.Unix(expires, 0).UTC().Format(time.RFC3339)
	result["gain_db"] = 0 // громкость ещё не измерена — узнаем, когда трек будет в медиатеке
	writeJSON(w, statusFor(result), result)
}

// ownStreamURL — ссылка на файл своего трека (загрузка): он всегда в
// медиатеке, скачивать нечего. Чужой черновик — как будто трека нет.
func (s *Server) ownStreamURL(w http.ResponseWriter, r *http.Request, id string) {
	if tracks, err := s.localTracks(viewerOf(r), []string{id}); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	} else if len(tracks) == 0 {
		writeError(w, http.StatusNotFound, "track not found")
		return
	}
	resp, err := s.libraryStreamURL(id)
	if err != nil || resp == nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// libraryStreamURL is the /media link of the track with this Deezer ID, or
// nil if the library doesn't have it.
func (s *Server) libraryStreamURL(deezerID string) (map[string]any, error) {
	var trackID, storageKey string
	var gainDB float64
	err := s.DB.QueryRow(`SELECT id, storage_key, gain_db FROM tracks WHERE deezer_id = $1`, deezerID).
		Scan(&trackID, &storageKey, &gainDB)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	s.touchTrack(trackID)
	url, expires := s.MediaSigner.URL(storageKey)
	return map[string]any{
		"status":     "duplicate",
		"track_id":   trackID,
		"url":        url,
		"expires_at": time.Unix(expires, 0).UTC().Format(time.RFC3339),
		"gain_db":    gainDB,
	}, nil
}

// touchTrack — трек включили: его аудио не удалять как неслушаемое
// (ingest.Housekeep). Не чаще раза в час на трек — не писать в базу на
// каждую выдачу ссылки.
func (s *Server) touchTrack(trackID string) {
	if _, err := s.DB.Exec(
		`UPDATE tracks SET last_played_at = now() WHERE id = $1 AND last_played_at < now() - interval '1 hour'`, trackID,
	); err != nil {
		log.Printf("track %s: last played: %v", trackID, err)
	}
}

// sameRecordingURL is the /media link of a library track with the same
// ISRC as the queued Deezer track, or nil.
func (s *Server) sameRecordingURL(deezerID string) (map[string]any, error) {
	var trackID, storageKey string
	var gainDB float64
	err := s.DB.QueryRow(
		`SELECT t.id, t.storage_key, t.gain_db FROM ingest_jobs j JOIN tracks t ON t.isrc = j.isrc
		  WHERE j.source_ref = $1 AND j.isrc <> '' ORDER BY t.id LIMIT 1`, "deezer:"+deezerID,
	).Scan(&trackID, &storageKey, &gainDB)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	s.touchTrack(trackID)
	url, expires := s.MediaSigner.URL(storageKey)
	return map[string]any{
		"track_id":   trackID,
		"url":        url,
		"expires_at": time.Unix(expires, 0).UTC().Format(time.RFC3339),
		"gain_db":    gainDB,
	}, nil
}

// handleDeezerStream (GET /api/stream/deezer/{id}?st=…&e=…) — the audio.
func (s *Server) handleDeezerStream(w http.ResponseWriter, r *http.Request) {
	deezerID := r.PathValue("id")
	if s.Streams == nil || !validDeezerID(deezerID) {
		http.NotFound(w, r)
		return
	}
	switch err := s.MediaSigner.Verify(streamPath(deezerID), r.URL.Query().Get("st"), r.URL.Query().Get("e")); err {
	case nil:
	case media.ErrExpired:
		http.Error(w, "link expired", http.StatusGone) // как nginx secure_link: плеер перевыпустит ссылку
		return
	default:
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if s.Streams.lookup(deezerID) == nil {
		// Скачанное уже убрано, а трек в медиатеке (ссылка ещё живая) — туда.
		if resp, err := s.libraryStreamURL(deezerID); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		} else if resp != nil {
			http.Redirect(w, r, resp["url"].(string), http.StatusFound)
			return
		}
		// Иначе (скачивание упало, сервер перезапускался) — качаем заново.
		if err := s.startStream(deezerID); err == sql.ErrNoRows {
			http.Error(w, "track is not queued", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	d := s.Streams.lookup(deezerID)
	if d == nil {
		http.Error(w, "could not get the audio from YouTube", http.StatusBadGateway) // упало мгновенно
		return
	}
	serveGrowingFile(w, r, d)
}

// startStream starts downloading the queued track deezerID, unless that's
// already under way. A pending job is held from the worker until the
// download is handed off to it; if the worker already has the job, the
// download is for listening only.
func (s *Server) startStream(deezerID string) error {
	if s.Streams.lookup(deezerID) != nil {
		return nil
	}
	var job streamJob
	var status string
	var manual, known sql.NullString
	err := s.DB.QueryRow(
		`SELECT id, status, artist_name, other_artists, title, album_title, duration_ms, manual_video_id, known_video_id
		   FROM ingest_jobs WHERE source_ref = $1`,
		"deezer:"+deezerID,
	).Scan(&job.id, &status, &job.want.Artist, pq.Array(&job.want.OtherArtists), &job.want.Title, &job.want.Album, &job.want.DurationMs,
		&manual, &known)
	if err != nil {
		return err
	}
	// Видео админа — только оно; уже скачанное раньше (файл удалили как
	// неслушаемый) — сначала оно, не открылось — поиск.
	switch {
	case manual.Valid:
		job.want.VideoID = manual.String
	case known.Valid:
		job.want.VideoID, job.want.SearchIfVideoFails = known.String, true
	}
	res, err := s.DB.Exec(
		`UPDATE ingest_jobs SET hold_until = now() + make_interval(secs => $2) WHERE id = $1 AND status = 'pending'`,
		job.id, streamDownloadTimeout.Seconds(),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		job.id = 0 // job уже у воркера (или готов) — воркеру не отдаём
	}
	s.Streams.get(s.DB, deezerID, job)
	return nil
}

// serveGrowingFile answers like http.ServeContent (Content-Length, single
// Range, 206/416), but from a file that is still being written: bytes that
// haven't arrived yet are waited for.
func serveGrowingFile(w http.ResponseWriter, r *http.Request, d *streamDownload) {
	ctx := r.Context()
	// Общий WriteTimeout сервера (cmd/api) не для потока: начала ждут, пока
	// освободится слот и найдётся запись, а дальше срок продлевается перед
	// каждой порцией.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(streamDownloadTimeout))
	st, err := d.wait(ctx, func(st streamState) bool { return st.size >= 0 || st.done || st.err != nil })
	if err != nil {
		return // клиент ушёл
	}
	if st.err != nil && st.written == 0 {
		http.Error(w, "could not get the audio from YouTube", http.StatusBadGateway)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "audio/mp4")
	h.Set("Cache-Control", "private, max-age=600")
	h.Set("X-Accel-Buffering", "no") // nginx не копит ответ в буфере — байты идут сразу

	start, end := int64(0), st.size-1
	status := http.StatusOK
	if st.size < 0 {
		// Размер неизвестен (не DASH) — без Content-Length и Range, просто поток.
		end = -1
	} else {
		h.Set("Accept-Ranges", "bytes")
		if rng := r.Header.Get("Range"); rng != "" {
			s, e, ok := parseRange(rng, st.size)
			if !ok {
				h.Set("Content-Range", fmt.Sprintf("bytes */%d", st.size))
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			start, end, status = s, e, http.StatusPartialContent
			h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, st.size))
		}
		h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}

	f, err := os.Open(d.path)
	if err != nil {
		return
	}
	defer f.Close()

	buf := make([]byte, 32<<10)
	pos := start
	for end < 0 || pos <= end {
		st, err := d.wait(ctx, func(st streamState) bool { return st.written > pos || st.done || st.err != nil })
		if err != nil {
			return
		}
		if st.written <= pos {
			// Скачивание кончилось (или упало) раньше, чем дошло до pos.
			// Ответ с Content-Length обрывается — клиент увидит ошибку сети.
			return
		}
		limit := st.written
		if end >= 0 {
			limit = min(limit, end+1)
		}
		for pos < limit {
			n, err := f.ReadAt(buf[:min(int64(len(buf)), limit-pos)], pos)
			if n > 0 {
				// Браузер на паузе перестаёт читать, а ответ должен дожить
				// до продолжения.
				_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Minute))
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				pos += int64(n)
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return
			}
			if n == 0 {
				break
			}
		}
		_ = rc.Flush()
	}
}

// parseRange handles a single "bytes=a-b", "bytes=a-" or "bytes=-n" range
// against size. Several ranges at once aren't supported (players don't
// ask for them): ok=false → 416.
func parseRange(header string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(header, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	if first == "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		return max(0, size-n), size - 1, true
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end = size - 1
	if last != "" {
		end, err = strconv.ParseInt(last, 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		end = min(end, size-1)
	}
	return start, end, true
}
