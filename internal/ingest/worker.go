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
	"syscall"
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
	OtherArtists        []string
	ISRC                sql.NullString
	ReplaceAudio        bool
	ManualVideoID       sql.NullString
	KnownVideoID        sql.NullString
	Explicit            sql.NullBool
}

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

func LoadJob(db *sql.DB, sourceRef string) (*Job, error) {
	return scanJob(db.QueryRow(`SELECT `+jobColumns+` FROM ingest_jobs WHERE source_ref = $1`, sourceRef))
}

func (j *Job) deezerTrackID() string {
	id, _ := strings.CutPrefix(j.SourceRef, "deezer:")
	return id
}

const minFreeSpace = 1 << 30

var freeSpace = func(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func DiskLow(dir string) bool {
	free, err := freeSpace(dir)
	return err == nil && free < minFreeSpace
}

func ClaimNextJob(db *sql.DB) (*Job, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	job, err := scanJob(tx.QueryRow(
		`SELECT ` + jobColumns + `
		   FROM ingest_jobs
		  WHERE status = 'pending' AND (hold_until IS NULL OR hold_until < now())
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

func ResetStaleJobs(db *sql.DB) (int64, error) {
	res, err := db.Exec(`UPDATE ingest_jobs SET status = 'pending', updated_at = now() WHERE status = 'processing'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

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

func ProcessJob(db *sql.DB, job *Job, stagingDir, storageDir string) {
	var trackID string
	var err error
	if safe.Run(fmt.Sprintf("job %d", job.ID), func() { trackID, err = processJob(db, job, stagingDir, storageDir) }) {
		err = errors.New("internal error (panic, see the worker log)")
	}
	if err != nil {
		log.Printf("job %d failed: %v", job.ID, err)
		text, attempt := err.Error(), 1
		switch {
		case errors.Is(err, ytdlp.ErrBlocked):
			text, attempt = "blocked: "+text, 0
		case errors.Is(err, ytdlp.ErrSignIn):
			text = "signin: " + text
		}
		_, _ = db.Exec(
			`UPDATE ingest_jobs SET status='failed', error=$1, attempts = attempts + $3, updated_at=now() WHERE id=$2`,
			text, job.ID, attempt,
		)
		return
	}
	MarkDone(db, job.ID, trackID)
	log.Printf("job %d done -> track %s", job.ID, trackID)
}

func MarkDone(db *sql.DB, jobID int64, trackID string) {
	_, _ = db.Exec(
		`UPDATE ingest_jobs SET status='done', result_track_id=$1, error=NULL, attempts=0, updated_at=now() WHERE id=$2`,
		trackID, jobID,
	)
}

func FailureKind(errText string) string {
	switch {
	case strings.HasPrefix(errText, "blocked:"):
		return "blocked"
	case strings.HasPrefix(errText, "signin:"):
		return "signin"
	case strings.HasPrefix(errText, "match:"):
		return "not_found"
	case strings.HasPrefix(errText, "download:"):
		return "download"
	case strings.HasPrefix(errText, "audio:"):
		return "audio"
	}
	return "other"
}

var (
	findMatches = ytdlp.FindMatches
	download    = ytdlp.Download
)

const (
	searchCandidates  = 15
	maxDownloadTries  = 3
	durationTolerance = 4000
)

const (
	searchTimeout    = 3 * time.Minute
	downloadTimeout  = 10 * time.Minute
	transcodeTimeout = 5 * time.Minute
)

func processJob(db *sql.DB, job *Job, stagingDir, storageDir string) (string, error) {
	rawPath := PrefetchPath(stagingDir, job.ID)
	if _, err := os.Stat(rawPath); err == nil {
		defer os.Remove(rawPath)
		return finishJob(db, job, rawPath, storageDir)
	}
	if key, lufs, gainDB, ok := sameRecording(db, job); ok && !job.ReplaceAudio && !job.ManualVideoID.Valid {
		trackID, _, err := saveTrack(db, job, key, lufs, gainDB)
		if err == nil {
			log.Printf("job %d: ISRC %s is already in the library, sharing its file", job.ID, job.ISRC.String)
		}
		return trackID, err
	}

	var err error
	switch {
	case job.ManualVideoID.Valid:
		rawPath, err = downloadFirst(db, job, []string{job.ManualVideoID.String}, stagingDir)
	case job.KnownVideoID.Valid:
		rawPath, err = downloadFirst(db, job, []string{job.KnownVideoID.String}, stagingDir)
		if err != nil && !errors.Is(err, ytdlp.ErrBlocked) {
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

func downloadFirst(db *sql.DB, job *Job, videoIDs []string, stagingDir string) (string, error) {
	var err, signInErr error
	var signInVideo string
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
			if n, err := RetryBlocked(db, true); err == nil && n > 0 {
				log.Printf("job %d: youtube lets the server in again, %d refused job(s) queued again", job.ID, n)
			}
			return rawPath, nil
		}
		log.Printf("job %d: download %s: %v", job.ID, videoID, err)
		if errors.Is(err, ytdlp.ErrBlocked) {
			break
		}
		if errors.Is(err, ytdlp.ErrSignIn) && signInErr == nil {
			signInErr, signInVideo = err, videoID
		}
	}
	if err == nil {
		err = errors.New("no video to download")
	}
	if signInErr != nil && !errors.Is(err, ytdlp.ErrBlocked) {
		err = signInErr
		_, _ = db.Exec(`UPDATE ingest_jobs SET video_id = $1 WHERE id = $2`, signInVideo, job.ID)
	}
	return "", fmt.Errorf("download: %w", err)
}

func replaceAudio(db *sql.DB, job *Job, storageDir, storageKey string, lufs, gainDB float64) (string, error) {
	rows, err := db.Query(
		`UPDATE tracks t SET storage_key = $1, loudness_lufs = $2, gain_db = $3,
		        audio_pinned = false, last_played_at = now()
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
		if err != sql.ErrNoRows {
			if err != nil {
				os.Remove(dstPath)
			}
			return trackID, err
		}
	}

	trackID, created, err := saveTrack(db, job, storageKey, lufs, gainDB)
	if err != nil || !created {
		os.Remove(dstPath)
	}
	return trackID, err
}

func SaveTrack(db *sql.DB, job *Job, storageKey string, lufs, gainDB float64) (trackID string, created bool, err error) {
	return saveTrack(db, job, storageKey, lufs, gainDB)
}

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
