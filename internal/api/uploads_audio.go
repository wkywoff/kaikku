package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"kaikku/internal/ingest"
)

// Аудио своих релизов (uploads.go): файл трека загружается сырым телом
// запроса (POST …/releases/{id}/audio?name=<имя файла>), проверяется
// ffprobe и перекодируется в .m4a (ingest.PrepareUpload). Сервер — 1 vCPU
// и предел памяти у API, поэтому обработка идёт по одной (audioSlot, общий
// с заменой аудио трека Deezer), а файл ждёт своей очереди на диске.

// audioSlot — одна обработка аудио за раз на весь API.
var audioSlot = make(chan struct{}, 1)

// slotWait — сколько запрос ждёт своей очереди. Дольше — 503 и повтор с
// фронта: иначе загрузка висела бы без ответа до таймаута nginx.
var slotWait = 45 * time.Second

// acquireAudioSlot ждёт очереди на обработку; false — не дождался (ответ уже дан).
func acquireAudioSlot(w http.ResponseWriter, r *http.Request) bool {
	select {
	case audioSlot <- struct{}{}:
		return true
	case <-time.After(slotWait):
		w.Header().Set("Retry-After", "20")
		writeError(w, http.StatusServiceUnavailable, "another file is being processed, try again shortly")
	case <-r.Context().Done():
	}
	return false
}

func releaseAudioSlot() { <-audioSlot }

// Место на диске: файл, его временная копия у nginx и результат не должны
// забить диск, на котором ещё база и всё остальное. freeSpace подменяют тесты.
const minFreeSpace = 1 << 30

var freeSpace = statFree

func statFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// Тесты подменяют обработку.
var (
	probeUpload   = ingest.ProbeUpload
	prepareUpload = ingest.PrepareUpload
	extractCover  = ingest.ExtractCover
)

// receivedAudio — загруженный и перекодированный файл, ещё без строки в базе.
type receivedAudio struct {
	key        string // uploads/<hex>.m4a
	info       *ingest.UploadInfo
	lufs, gain float64
	durationMs int
	src        string // исходник (временный файл) — для обложки из тегов
}

// receiveAudio принимает тело запроса во временный файл на диске, ждёт
// очереди и перекодирует. Ответ при ошибке даёт сам. Вызвавший удаляет
// a.src (os.Remove) и отпускает слот (releaseAudioSlot), если ok.
func (s *Server) receiveAudio(w http.ResponseWriter, r *http.Request) (a receivedAudio, ok bool) {
	if s.StorageDir == "" {
		writeError(w, http.StatusServiceUnavailable, "storage is not configured")
		return a, false
	}
	if r.ContentLength > maxAudioUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "audio must be at most 200 MB")
		return a, false
	}
	if free, err := freeSpace(s.StorageDir); err == nil && free < minFreeSpace+2*uint64(max(r.ContentLength, 0)) {
		writeError(w, http.StatusInsufficientStorage, "the server is running out of disk space")
		return a, false
	}
	// Файл в сотни МБ, очередь и перекодирование — дольше общих таймаутов
	// сервера (cmd/api); nginx ждёт /api/uploads/ 420 с.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(audioUploadTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(slotWait + audioUploadTimeout + 30*time.Second))

	src, err := os.CreateTemp(s.StorageDir, ".upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save audio")
		return a, false
	}
	a.src = src.Name()
	_, err = io.Copy(src, http.MaxBytesReader(w, r.Body, maxAudioUpload))
	src.Close()
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		os.Remove(a.src)
		writeError(w, http.StatusRequestEntityTooLarge, "audio must be at most 200 MB")
		return a, false
	} else if err != nil {
		os.Remove(a.src)
		writeError(w, http.StatusBadRequest, "upload interrupted")
		return a, false
	}
	if !acquireAudioSlot(w, r) {
		os.Remove(a.src)
		return a, false
	}
	fail := func(status int, msg string) (receivedAudio, bool) {
		releaseAudioSlot()
		os.Remove(a.src)
		writeError(w, status, msg)
		return a, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), audioUploadTimeout)
	defer cancel()
	if a.info, err = probeUpload(ctx, a.src); err != nil {
		log.Printf("upload %q: %v", r.URL.Query().Get("name"), err)
		return fail(http.StatusBadRequest, "this is not an audio file we can read (FLAC, WAV, AIFF, MP3, M4A, OGG or Opus)")
	}
	if a.info.Duration < 1 {
		return fail(http.StatusBadRequest, "the file has no sound")
	}
	if err := os.MkdirAll(filepath.Join(s.StorageDir, "uploads"), 0o755); err != nil {
		return fail(http.StatusInternalServerError, "could not save audio")
	}
	a.key = "uploads/" + randomKey() + ".m4a"
	if a.lufs, a.gain, a.durationMs, err = prepareUpload(ctx, a.src, filepath.Join(s.StorageDir, a.key), a.info); err != nil {
		log.Printf("upload %q: %v", r.URL.Query().Get("name"), err)
		return fail(http.StatusBadRequest, "could not convert the audio")
	}
	return a, true
}

// handleAddReleaseAudio — новый трек релиза из файла. Название — из тега
// или имени файла, номер — следующий; остальное правится в окне загрузки.
// Если у релиза ещё нет обложки, берётся картинка из файла.
func (s *Server) handleAddReleaseAudio(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	a, ok := s.receiveAudio(w, r)
	if !ok {
		return
	}
	defer os.Remove(a.src)
	defer releaseAudioSlot()

	title := a.info.Tags.Title
	if title == "" {
		title = titleFromFilename(r.URL.Query().Get("name"))
	}
	if tooLong(title) {
		title = string([]rune(title)[:maxTitle])
	}
	trackID, err := s.insertOwnTrack(rel, title, a)
	if err != nil {
		s.removeStorageFiles(a.key)
		log.Printf("upload to %s: %v", rel.id, err)
		writeError(w, http.StatusInternalServerError, "could not save the track")
		return
	}
	if a.info.Tags.Lyrics != "" && s.Lyrics != nil { // текст из тегов файла
		if err := s.saveOwnLyrics(r.Context(), trackID, a.info.Tags.Lyrics); err != nil {
			log.Printf("lyrics of %s from tags: %v", trackID, err)
		}
	}
	resp := map[string]any{
		"track": uploadTrack{ID: trackID, Title: title, DurationMs: a.durationMs},
		"tags":  a.info.Tags,
	}
	if url := s.coverFromFile(r, rel, a); url != "" {
		resp["cover_url"] = url
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) insertOwnTrack(rel releaseRow, title string, a receivedAudio) (string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// FOR UPDATE: два файла, загруженные разом, не получат один номер.
	if err := tx.QueryRow(`SELECT 1 FROM albums WHERE id = $1 FOR UPDATE`, rel.libraryID).Scan(new(int)); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(
		`INSERT INTO tracks (deezer_id, album_id, title, duration_ms, track_no, disc_no, storage_key, loudness_lufs, gain_db, isrc, edited)
		 SELECT 'k' || nextval('kaikku_ids'), $1, $2, $3, coalesce(max(track_no), 0) + 1, 1, $4, $5, $6, '', true
		   FROM tracks WHERE album_id = $1
		 RETURNING deezer_id`,
		rel.libraryID, title, a.durationMs, a.key, a.lufs, a.gain).Scan(&id); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// coverFromFile ставит картинку из файла обложкой релиза, если своей у
// него ещё нет; "" — не поставили.
func (s *Server) coverFromFile(r *http.Request, rel releaseRow, a receivedAudio) string {
	if !a.info.HasCover {
		return ""
	}
	var has bool
	if s.DB.QueryRow(`SELECT cover_key IS NOT NULL FROM albums WHERE id = $1`, rel.libraryID).Scan(&has) != nil || has {
		return ""
	}
	key := filepath.Join("covers", "custom", randomKey()+".jpg")
	if err := os.MkdirAll(filepath.Join(s.StorageDir, "covers", "custom"), 0o755); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := extractCover(ctx, a.src, filepath.Join(s.StorageDir, key), a.info); err != nil {
		log.Printf("cover from %q: %v", r.URL.Query().Get("name"), err)
		return ""
	}
	if err := s.setReleaseCover(rel, key, false); err != nil { // своя появилась раньше — она главнее
		s.removeStorageFiles(key)
		return ""
	}
	return "/media/" + key
}

// handleReplaceTrackAudio — другой файл того же трека: ID, лайки, плейлисты и
// история остаются.
func (s *Server) handleReplaceTrackAudio(w http.ResponseWriter, r *http.Request) {
	rel, trackID, ok := s.ownTrackFor(w, r)
	if !ok {
		return
	}
	a, ok := s.receiveAudio(w, r)
	if !ok {
		return
	}
	defer os.Remove(a.src)
	defer releaseAudioSlot()
	var old string
	err := s.DB.QueryRow(
		`UPDATE tracks t SET storage_key = $2, loudness_lufs = $3, gain_db = $4, duration_ms = $5
		   FROM (SELECT storage_key FROM tracks WHERE deezer_id = $1 FOR UPDATE) prev
		  WHERE t.deezer_id = $1 RETURNING prev.storage_key`,
		trackID, a.key, a.lufs, a.gain, a.durationMs).Scan(&old)
	if err != nil {
		s.removeStorageFiles(a.key)
		writeError(w, http.StatusInternalServerError, "could not save the track")
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(old)...)
	s.syncLocalSnapshots(rel.id)
	writeJSON(w, http.StatusOK, map[string]any{"duration_ms": a.durationMs, "tags": a.info.Tags})
}

// handleDeleteOwnTrack убирает трек из релиза (и из медиатеки слушателей).
// Последний трек опубликованного релиза удалить нельзя — релиз без треков
// был бы пустой страницей; удаляется весь релиз.
func (s *Server) handleDeleteOwnTrack(w http.ResponseWriter, r *http.Request) {
	rel, trackID, ok := s.ownTrackFor(w, r)
	if !ok {
		return
	}
	if !rel.draft {
		var n int
		s.DB.QueryRow(`SELECT count(*) FROM tracks WHERE album_id = $1`, rel.libraryID).Scan(&n)
		if n <= 1 {
			writeError(w, http.StatusConflict, "a published release needs at least one track")
			return
		}
	}
	keys, err := s.deleteOwn(rel.id, rel.libraryID, trackID)
	if err != nil {
		log.Printf("delete track %s: %v", trackID, err)
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(keys...)...)
	w.WriteHeader(http.StatusNoContent)
}

// ownTrackFor — свой трек и его релиз, если зритель вправе его править.
func (s *Server) ownTrackFor(w http.ResponseWriter, r *http.Request) (releaseRow, string, bool) {
	trackID := r.PathValue("id")
	var albumID string
	if !isLocalID(trackID) || s.DB.QueryRow(
		`SELECT a.deezer_id FROM tracks t JOIN albums a ON a.id = t.album_id WHERE t.deezer_id = $1`, trackID).Scan(&albumID) != nil {
		writeError(w, http.StatusNotFound, "track not found")
		return releaseRow{}, "", false
	}
	rel, ok := s.releaseByID(w, r, albumID)
	return rel, trackID, ok
}

// titleFromFilename — название трека из имени файла: без расширения, без
// номера впереди («01 - », «07. », «3_») и с пробелами вместо «_».
var leadingNumber = regexp.MustCompile(`^\d{1,3}(\s*[-._)\]]\s*|\s+)`)

func titleFromFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if ext := filepath.Ext(name); len(ext) <= 5 {
		name = strings.TrimSuffix(name, ext)
	}
	title := strings.TrimSpace(strings.ReplaceAll(leadingNumber.ReplaceAllString(name, ""), "_", " "))
	if title == "" || title == "." {
		title = strings.TrimSpace(name)
	}
	if title == "" {
		return "Track"
	}
	return title
}

// SweepUploads убирает при старте API то, что оставили оборванные загрузки:
// временные файлы и перекодированные треки без строки в базе (рестарт
// между обработкой и записью). Свежие (младше часа) не трогает — их могла
// оставить ещё идущая загрузка в другом процессе.
func (s *Server) SweepUploads() {
	if s.StorageDir == "" {
		return
	}
	old := func(path string) bool {
		st, err := os.Stat(path)
		return err == nil && time.Since(st.ModTime()) > time.Hour
	}
	tmp, _ := filepath.Glob(filepath.Join(s.StorageDir, ".upload-*"))
	parts, _ := filepath.Glob(filepath.Join(s.StorageDir, "uploads", "*.part"))
	for _, p := range append(tmp, parts...) {
		if old(p) {
			os.Remove(p)
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.StorageDir, "uploads", "*.m4a"))
	var keys []string
	for _, f := range files {
		if old(f) {
			keys = append(keys, "uploads/"+filepath.Base(f))
		}
	}
	if free := s.unreferencedAudio(keys...); len(free) > 0 {
		log.Printf("uploads: removing %d files nobody uses: %s", len(free), strings.Join(free, ", "))
		s.removeStorageFiles(free...)
	}
}
