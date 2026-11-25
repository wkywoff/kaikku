package ingest

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"
)

func Housekeep(db *sql.DB, storageDir string, keep time.Duration) {
	if res, err := db.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, updated_at = now()
		  WHERE status = 'failed' AND attempts < $1 AND updated_at < now() - interval '20 hours'
		    AND COALESCE(error, '') NOT LIKE 'blocked:%'`, MaxAttempts,
	); err != nil {
		log.Printf("housekeep: retry: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("housekeep: retrying %d failed job(s)", n)
	}
	if n, err := RetryBlocked(db, false); err != nil {
		log.Printf("housekeep: retry blocked: %v", err)
	} else if n > 0 {
		log.Printf("housekeep: youtube refused the server, trying one of those jobs again")
	}

	if res, err := db.Exec(
		`DELETE FROM ingest_jobs j
		  WHERE (j.status = 'done' AND j.result_track_id IS NULL AND j.known_video_id IS NULL
		         AND j.updated_at < now() - interval '7 days')
		     OR (j.status = 'failed' AND j.attempts >= $1 AND j.updated_at < now() - interval '90 days'
		         AND NOT EXISTS (SELECT 1 FROM liked_tracks l WHERE 'deezer:' || l.deezer_id = j.source_ref)
		         AND NOT EXISTS (SELECT 1 FROM playlist_items p WHERE 'deezer:' || p.deezer_id = j.source_ref))`, MaxAttempts,
	); err != nil {
		log.Printf("housekeep: prune: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("housekeep: removed %d dead job(s)", n)
	}

	evictAudio(db, storageDir, keep)

	if _, err := db.Exec(`DELETE FROM deezer_cache WHERE expires_at < now()`); err != nil {
		log.Printf("housekeep: deezer cache: %v", err)
	}
}

func RetryBlocked(db *sql.DB, all bool) (int64, error) {
	var limit any = 1
	if all {
		limit = nil
	}
	res, err := db.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, updated_at = now()
		  WHERE id IN (SELECT id FROM ingest_jobs WHERE status = 'failed' AND error LIKE 'blocked:%'
		                ORDER BY updated_at LIMIT $1)`, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func RetrySignIn(db *sql.DB) (int64, error) {
	res, err := db.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, attempts = 0, updated_at = now()
		  WHERE status = 'failed' AND error LIKE 'signin:%'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const MaxAttempts = 3

const evictAudioBatch = 200

func evictAudio(db *sql.DB, storageDir string, keep time.Duration) {
	rows, err := db.Query(
		`SELECT t.id FROM tracks t
		  WHERE NOT t.audio_pinned AND NOT t.edited AND t.deezer_id ~ '^[0-9]+$'
		    AND t.last_played_at < now() - make_interval(secs => $1)
		    AND NOT EXISTS (SELECT 1 FROM ingest_jobs j
		                     WHERE j.source_ref = 'deezer:' || t.deezer_id AND j.status IN ('pending', 'processing'))
		  ORDER BY t.last_played_at LIMIT $2`, keep.Seconds(), evictAudioBatch)
	if err != nil {
		log.Printf("housekeep: evict: %v", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("housekeep: evict: reading rows: %v", err)
	}

	var n int
	var freed int64
	for _, id := range ids {
		key, err := evictTrack(db, id)
		if err != nil {
			log.Printf("housekeep: evict track %s: %v", id, err)
			continue
		}
		n++
		var others int
		if db.QueryRow(`SELECT count(*) FROM tracks WHERE storage_key = $1`, key).Scan(&others) != nil || others > 0 {
			continue
		}
		path := filepath.Join(storageDir, key)
		if st, err := os.Stat(path); err == nil {
			freed += st.Size()
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("housekeep: removing %s: %v", key, err)
		}
	}
	if n > 0 {
		log.Printf("housekeep: removed %d track(s) nobody played for %s, freed %.1f MB", n, keep, float64(freed)/(1<<20))
	}
}

func evictTrack(db *sql.DB, trackID string) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var key, deezerID string
	if err := tx.QueryRow(`DELETE FROM tracks WHERE id = $1 RETURNING storage_key, deezer_id`, trackID).Scan(&key, &deezerID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(
		`UPDATE ingest_jobs SET known_video_id = COALESCE(video_id, known_video_id), result_track_id = NULL, updated_at = now()
		  WHERE source_ref = $1`, "deezer:"+deezerID); err != nil {
		return "", err
	}
	return key, tx.Commit()
}
