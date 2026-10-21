package ingest

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Housekeep — уборка воркера (раз в час, cmd/worker):
//
//   - упавшие задачи повторяются раз в сутки, пока попыток меньше
//     MaxAttempts (YouTube или yt-dlp могли починиться, ролик — появиться);
//   - мёртвые задачи удаляются: готовые, чей трек удалили, и безнадёжно
//     упавшие, на которые никто не ссылается;
//   - аудио, которое никто не включал keep, удаляется с диска (evictAudio);
//   - устаревший кеш Deezer удаляется из базы (deezer_cache).
func Housekeep(db *sql.DB, storageDir string, keep time.Duration) {
	if res, err := db.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, updated_at = now()
		  WHERE status = 'failed' AND attempts < $1 AND updated_at < now() - interval '20 hours'`, MaxAttempts,
	); err != nil {
		log.Printf("housekeep: retry: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("housekeep: retrying %d failed job(s)", n)
	}

	// Готовые без трека, но с известным видео — это удалённое неслушаемое
	// аудио: по ним трек скачается заново с того же видео, их не трогаем.
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

// MaxAttempts — сколько раз задача может упасть, прежде чем Housekeep
// перестанет её повторять. Такие пропадают и из «не нашлось»: ссылку или
// свой файл для них админ даёт из правки трека на странице альбома.
const MaxAttempts = 3

// evictAudioBatch — сколько треков за проход: удаление файлов не должно
// надолго занимать воркер.
const evictAudioBatch = 200

// evictAudio удаляет файлы треков Deezer, которые никто не включал keep.
// Строка трека уходит (лайки, плейлисты и история — по Deezer ID, им она не
// нужна), задача остаётся готовой без трека и помнит видео: следующее
// включение поставит её в очередь, и трек скачается с того же ролика. Не
// трогает свои загрузки (k…), файл админа (audio_pinned), правленые строки
// и треки, которые сейчас качаются. Файл, который делят несколько релизов
// (одна запись), удаляется только вместе с последним из них.
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

// evictTrack удаляет строку трека, запомнив в его задаче видео; возвращает
// ключ файла.
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
