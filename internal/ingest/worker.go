package ingest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/safe"
	"kaikku/internal/ytdlp"
)

type Job struct {
	ID                  int64
	SourceRef           string
	Title               string
	ArtistName          string
	ArtistDeezerID      sql.NullString
	AlbumArtistName     string
	AlbumArtistDeezerID sql.NullString
	AlbumTitle          string
	AlbumDeezerID       sql.NullString
	TrackNo             sql.NullInt32
	DiscNo              sql.NullInt32
	DurationMs          int
	CoverURL            sql.NullString
	ReleaseDate         sql.NullString
	AlbumRecordType     sql.NullString
	OtherArtists        []string // фиты — для поиска на YouTube
	ISRC                sql.NullString
	// ReplaceAudio — перекачка трека, который уже в медиатеке (админ): файл
	// заменяется в строке трека, а не заводится новый трек.
	ReplaceAudio bool
	// ManualVideoID — видео, которое указал админ: без поиска (0028).
	ManualVideoID sql.NullString
	// KnownVideoID — видео, с которого трек уже качали (файл удалили как
	// неслушаемый, 0029): сначала оно, не скачалось — обычный поиск.
	KnownVideoID sql.NullString
	// Explicit — метка Deezer, для выбора версии на YouTube Music (0037).
	Explicit sql.NullBool
}

// jobColumns и scanJob — одна задача целиком (ClaimNextJob, LoadJob).
const jobColumns = `id, source_ref, title, artist_name, artist_deezer_id,
	album_artist_name, album_artist_deezer_id, album_title, album_deezer_id,
	track_no, disc_no, duration_ms, cover_url, to_char(release_date, 'YYYY-MM-DD'), album_record_type,
	other_artists, isrc, replace_audio, manual_video_id, known_video_id, explicit`

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.SourceRef, &job.Title, &job.ArtistName, &job.ArtistDeezerID,
		&job.AlbumArtistName, &job.AlbumArtistDeezerID, &job.AlbumTitle, &job.AlbumDeezerID,
		&job.TrackNo, &job.DiscNo, &job.DurationMs, &job.CoverURL, &job.ReleaseDate, &job.AlbumRecordType,
		pq.Array(&job.OtherArtists), &job.ISRC, &job.ReplaceAudio, &job.ManualVideoID, &job.KnownVideoID, &job.Explicit)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// LoadJob — задача по source_ref ("deezer:<id>"); sql.ErrNoRows — её нет.
func LoadJob(db *sql.DB, sourceRef string) (*Job, error) {
	return scanJob(db.QueryRow(`SELECT `+jobColumns+` FROM ingest_jobs WHERE source_ref = $1`, sourceRef))
}

// deezerTrackID extracts the Deezer ID from source_ref ("deezer:<id>").
func (j *Job) deezerTrackID() string {
	id, _ := strings.CutPrefix(j.SourceRef, "deezer:")
	return id
}

// ClaimNextJob atomically claims one pending job and marks it 'processing'.
// Returns (nil, nil) if there's nothing to do.
func ClaimNextJob(db *sql.DB) (*Job, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Повторы упавших (attempts > 0, Housekeep) — после новых запросов.
	job, err := scanJob(tx.QueryRow(
		`SELECT ` + jobColumns + `
		   FROM ingest_jobs
		  WHERE status = 'pending' AND (hold_until IS NULL OR hold_until < now()) -- hold: трек качает API (stream.go)
		  ORDER BY attempts, id LIMIT 1 FOR UPDATE SKIP LOCKED`,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(
		`UPDATE ingest_jobs SET status = 'processing', updated_at = now() WHERE id = $1`, job.ID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

// ResetStaleJobs returns jobs left in 'processing' by a crashed or killed
// worker to the queue. Call it once at startup, before claiming anything —
// it assumes this is the only worker process (its goroutines haven't
// started yet); with several processes a heartbeat column would be needed.
func ResetStaleJobs(db *sql.DB) (int64, error) {
	res, err := db.Exec(`UPDATE ingest_jobs SET status = 'pending', updated_at = now() WHERE status = 'processing'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CleanStaging removes leftovers of downloads interrupted by a crash. Only
// the worker writes to stagingDir, so at startup everything there is stale
// (the API only adds finished PrefetchPath files; losing one just means the
// job downloads the track itself).
func CleanStaging(stagingDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(stagingDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ProcessJob runs match -> download -> transcode -> catalog upsert, then
// marks the job done/failed. Errors are logged and recorded on the row, not
// returned — the poll loop always continues to the next job.
func ProcessJob(db *sql.DB, job *Job, stagingDir, storageDir string) {
	var trackID string
	var err error
	// Паника в одном job'е не роняет воркер: job — failed, цикл идёт дальше.
	if safe.Run(fmt.Sprintf("job %d", job.ID), func() { trackID, err = processJob(db, job, stagingDir, storageDir) }) {
		err = errors.New("internal error (panic, see the worker log)")
	}
	if err != nil {
		log.Printf("job %d failed: %v", job.ID, err)
		_, _ = db.Exec(
			`UPDATE ingest_jobs SET status='failed', error=$1, attempts = attempts + 1, updated_at=now() WHERE id=$2`,
			err.Error(), job.ID,
		)
		return
	}
	MarkDone(db, job.ID, trackID)
	log.Printf("job %d done -> track %s", job.ID, trackID)
}

// MarkDone — задача готова, трек в медиатеке.
func MarkDone(db *sql.DB, jobID int64, trackID string) {
	_, _ = db.Exec(
		`UPDATE ingest_jobs SET status='done', result_track_id=$1, error=NULL, attempts=0, updated_at=now() WHERE id=$2`,
		trackID, jobID,
	)
}

// FailureKind — вид ошибки задачи по префиксу, который пишет processJob:
// not_found (на YouTube не нашлось), download, audio или other.
func FailureKind(errText string) string {
	switch {
	case strings.HasPrefix(errText, "match:"):
		return "not_found"
	case strings.HasPrefix(errText, "download:"):
		return "download"
	case strings.HasPrefix(errText, "audio:"):
		return "audio"
	}
	return "other"
}

// Поиск и скачивание на YouTube; тесты подменяют.
var (
	findMatches = ytdlp.FindMatches
	download    = ytdlp.Download
)

// Матчинг YouTube по длительности — сколько кандидатов смотрим и какое
// расхождение с эталонной длительностью Deezer ещё считаем той же записью
// (ремастеры/разные заливки отличаются на секунды, не на миллисекунды).
const (
	searchCandidates  = 15 // одна страница плоского поиска, ~1.5 с
	maxDownloadTries  = 3
	durationTolerance = 4000 // мс
)

// Верхние границы на внешние процессы: один зависший yt-dlp/ffmpeg иначе
// навсегда останавливает единственный воркер.
const (
	searchTimeout    = 3 * time.Minute
	downloadTimeout  = 10 * time.Minute
	transcodeTimeout = 5 * time.Minute
)

func processJob(db *sql.DB, job *Job, stagingDir, storageDir string) (string, error) {
	rawPath := PrefetchPath(stagingDir, job.ID)
	if _, err := os.Stat(rawPath); err == nil {
		// API уже скачал трек, пока отдавал его потоком; video_id он записал сам.
		defer os.Remove(rawPath)
		return finishJob(db, job, rawPath, storageDir)
	}
	if key, lufs, gainDB, ok := sameRecording(db, job); ok && !job.ReplaceAudio && !job.ManualVideoID.Valid {
		// Та же запись (ISRC) на другом релизе уже скачана: тот же файл, без
		// поиска и скачивания. Файл общий — удаление трека его не трогает,
		// пока на него ссылается другой (internal/api, unreferencedAudio).
		// Перекачке он не годится: его и хотят заменить.
		trackID, _, err := saveTrack(db, job, key, lufs, gainDB)
		if err == nil {
			log.Printf("job %d: ISRC %s is already in the library, sharing its file", job.ID, job.ISRC.String)
		}
		return trackID, err
	}

	var err error
	switch {
	case job.ManualVideoID.Valid: // админ указал видео — его и только его
		rawPath, err = downloadFirst(db, job, []string{job.ManualVideoID.String}, stagingDir)
	case job.KnownVideoID.Valid: // с него уже качали; удалили — ищем заново
		if rawPath, err = downloadFirst(db, job, []string{job.KnownVideoID.String}, stagingDir); err != nil {
			log.Printf("job %d: known video %s: %v, searching again", job.ID, job.KnownVideoID.String, err)
			rawPath, err = searchAndDownload(db, job, stagingDir)
		}
	default:
		rawPath, err = searchAndDownload(db, job, stagingDir)
	}
	if err != nil {
		return "", err
	}
	defer os.Remove(rawPath)
	return finishJob(db, job, rawPath, storageDir)
}

func searchAndDownload(db *sql.DB, job *Job, stagingDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	videoIDs, err := findMatches(ctx,
		ytdlp.Want{Artist: job.ArtistName, OtherArtists: job.OtherArtists, Title: job.Title, Album: job.AlbumTitle, DurationMs: job.DurationMs,
			Explicit: ytdlp.NullBool(job.Explicit)},
		searchCandidates, durationTolerance)
	cancel()
	if err != nil {
		return "", fmt.Errorf("match: %w", err)
	}
	return downloadFirst(db, job, videoIDs, stagingDir)
}

// downloadFirst скачивает первое из videoIDs, которое скачивается: плоский
// поиск не проверяет доступность ролика (гео/возраст/удалён) — если лучший
// не скачался, пробуем следующих.
func downloadFirst(db *sql.DB, job *Job, videoIDs []string, stagingDir string) (string, error) {
	var err error
	for i, videoID := range videoIDs {
		if i == maxDownloadTries {
			break
		}
		_, _ = db.Exec(`UPDATE ingest_jobs SET video_id = $1 WHERE id = $2`, videoID, job.ID)
		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		var rawPath string
		rawPath, err = download(ctx, videoID, stagingDir, randomID())
		cancel()
		if err == nil {
			return rawPath, nil
		}
		log.Printf("job %d: download %s: %v", job.ID, videoID, err)
	}
	if err == nil {
		err = errors.New("no video to download")
	}
	return "", fmt.Errorf("download: %w", err)
}

// replaceAudio points the existing track at the new file and removes the old
// one unless another track (the same recording on another release) uses
// it. sql.ErrNoRows — the track is gone.
func replaceAudio(db *sql.DB, job *Job, storageDir, storageKey string, lufs, gainDB float64) (string, error) {
	// Файл меняется у всех треков на нём: та же запись (ISRC) на другом
	// релизе делит файл (sameRecording) и иначе осталась бы на старом — так
	// же, как при загрузке файла админом (internal/api).
	rows, err := db.Query(
		`UPDATE tracks t SET storage_key = $1, loudness_lufs = $2, gain_db = $3,
		        audio_pinned = false, last_played_at = now() -- файл снова с YouTube
		   FROM (SELECT storage_key FROM tracks WHERE deezer_id = $4) prev
		  WHERE t.storage_key = prev.storage_key
		  RETURNING t.id, t.deezer_id, prev.storage_key`,
		storageKey, lufs, gainDB, job.deezerTrackID(),
	)
	if err != nil {
		return "", err
	}
	var trackID, oldKey string
	shared := 0
	for rows.Next() {
		var id, dz string
		if err := rows.Scan(&id, &dz, &oldKey); err != nil {
			rows.Close()
			return "", err
		}
		if dz == job.deezerTrackID() {
			trackID = id
		} else {
			shared++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if trackID == "" {
		return "", sql.ErrNoRows
	}
	if shared > 0 {
		log.Printf("job %d: the same recording on %d other release(s) gets the new audio too", job.ID, shared)
	}
	_, _ = db.Exec(`UPDATE ingest_jobs SET replace_audio = false WHERE id = $1`, job.ID)
	var others int
	if db.QueryRow(`SELECT count(*) FROM tracks WHERE storage_key = $1`, oldKey).Scan(&others) == nil && others == 0 {
		if err := os.Remove(filepath.Join(storageDir, oldKey)); err != nil && !os.IsNotExist(err) {
			log.Printf("job %d: removing replaced audio %s: %v", job.ID, oldKey, err)
		}
	}
	log.Printf("job %d: audio replaced (%s -> %s)", job.ID, oldKey, storageKey)
	return trackID, nil
}

// sameRecording finds the library file of the same recording released
// elsewhere (same ISRC, another Deezer track).
func sameRecording(db *sql.DB, job *Job) (storageKey string, lufs, gainDB float64, ok bool) {
	if !job.ISRC.Valid || job.ISRC.String == "" {
		return "", 0, 0, false
	}
	err := db.QueryRow(
		`SELECT storage_key, COALESCE(loudness_lufs, 0), gain_db FROM tracks
		  WHERE isrc = $1 AND deezer_id <> $2 ORDER BY id LIMIT 1`, job.ISRC.String, job.deezerTrackID(),
	).Scan(&storageKey, &lufs, &gainDB)
	return storageKey, lufs, gainDB, err == nil
}

// finishJob turns downloaded audio into a library track: remux + loudness
// (PrepareAudio), then the catalog rows.
func finishJob(db *sql.DB, job *Job, rawPath, storageDir string) (string, error) {
	storageKey := randomID() + ".m4a"
	dstPath := filepath.Join(storageDir, storageKey)
	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	lufs, gainDB, err := PrepareAudio(ctx, rawPath, dstPath)
	cancel()
	if err != nil {
		return "", fmt.Errorf("audio: %w", err)
	}

	if job.ReplaceAudio {
		trackID, err := replaceAudio(db, job, storageDir, storageKey, lufs, gainDB)
		if err != nil {
			os.Remove(dstPath)
		}
		if err != sql.ErrNoRows {
			return trackID, err
		}
		// Трек успели удалить — заводится заново, как обычное скачивание.
	}

	trackID, created, err := saveTrack(db, job, storageKey, lufs, gainDB)
	if err != nil || !created {
		os.Remove(dstPath)
	}
	return trackID, err
}

// SaveTrack is saveTrack for the API: an admin's file for a track that
// was never found on YouTube becomes the track (handleUploadTrackAudio).
func SaveTrack(db *sql.DB, job *Job, storageKey string, lufs, gainDB float64) (trackID string, created bool, err error) {
	return saveTrack(db, job, storageKey, lufs, gainDB)
}

// saveTrack upserts the track's artist/album and inserts the track. If a
// track with the same Deezer ID already exists (the job was re-queued after
// a crash that happened after the insert), the existing row wins:
// created=false and the caller drops the freshly transcoded file.
func saveTrack(db *sql.DB, job *Job, storageKey string, lufs, gainDB float64) (trackID string, created bool, err error) {
	trackArtistID, err := findOrCreateArtist(db, job.ArtistDeezerID.String, job.ArtistName)
	if err != nil {
		return "", false, fmt.Errorf("track artist upsert: %w", err)
	}
	albumArtistID := trackArtistID
	if job.AlbumArtistDeezerID.String != job.ArtistDeezerID.String || job.AlbumArtistName != job.ArtistName {
		albumArtistID, err = findOrCreateArtist(db, job.AlbumArtistDeezerID.String, job.AlbumArtistName)
		if err != nil {
			return "", false, fmt.Errorf("album artist upsert: %w", err)
		}
	}
	albumID, err := findOrCreateAlbum(db, job.AlbumDeezerID.String, albumArtistID, job.AlbumTitle, job.ReleaseDate, job.AlbumRecordType)
	if err != nil {
		return "", false, fmt.Errorf("album upsert: %w", err)
	}

	err = db.QueryRow(
		`INSERT INTO tracks (album_id, artist_id, title, duration_ms, track_no, disc_no, storage_key, loudness_lufs, gain_db, deezer_id, isrc)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (deezer_id) DO NOTHING
		 RETURNING id`,
		albumID, trackArtistID, job.Title, job.DurationMs, job.TrackNo, job.DiscNo, storageKey, lufs, gainDB,
		nullable(job.deezerTrackID()), nullable(job.ISRC.String),
	).Scan(&trackID)
	switch {
	case err == sql.ErrNoRows:
		err = db.QueryRow(`SELECT id FROM tracks WHERE deezer_id = $1`, job.deezerTrackID()).Scan(&trackID)
		if err != nil {
			return "", false, fmt.Errorf("looking up existing track: %w", err)
		}
	case err != nil:
		return "", false, fmt.Errorf("insert track: %w", err)
	default:
		created = true
		// Админ правил трек, пока его не было в медиатеке (track_edits,
		// миграция 0010), — правка переходит в строку трека.
		if res, err := db.Exec(
			`UPDATE tracks t SET title = e.title, track_no = COALESCE(e.track_no, t.track_no),
			        disc_no = COALESCE(e.disc_no, t.disc_no), edited = true
			   FROM track_edits e WHERE t.id = $1 AND e.deezer_id = t.deezer_id`, trackID,
		); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				db.Exec(`DELETE FROM track_edits WHERE deezer_id = $1`, job.deezerTrackID())
			}
		}
	}

	return trackID, created, nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
