package ingest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"kaikku/internal/ytdlp"
)

// fakeYouTube подменяет поиск и скачивание: поиск отдаёт found (или
// ErrNoMatch, если пусто), скачивание — 3 с синуса для id без префикса
// gone. Возвращает, что спрашивали.
func fakeYouTube(t *testing.T, found ...string) (searches *int, downloads *[]string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	var n int
	var got []string
	oldFind, oldDownload := findMatches, download
	findMatches = func(context.Context, ytdlp.Want, int, int) ([]string, error) {
		n++
		if len(found) == 0 {
			return nil, errors.New("youtube search returned no results")
		}
		return found, nil
	}
	download = func(ctx context.Context, videoID, dir, name string) (string, error) {
		got = append(got, videoID)
		if strings.HasPrefix(videoID, "gone") {
			return "", errors.New("Video unavailable")
		}
		path := filepath.Join(dir, name+".m4a")
		if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
			"-c:a", "aac", "-f", "mp4", path).CombinedOutput(); err != nil {
			return "", errors.New(string(out))
		}
		return path, nil
	}
	t.Cleanup(func() { findMatches, download = oldFind, oldDownload })
	return &n, &got
}

func TestProcessJob_VideoChoice(t *testing.T) {
	db := openTestDB(t)
	staging, storage := t.TempDir(), t.TempDir()
	run := func(ref, manual, known string) (status, errText, videoID string, attempts int) {
		t.Helper()
		db.Exec(`DELETE FROM tracks WHERE deezer_id = $1`, strings.TrimPrefix(ref, "deezer:"))
		db.Exec(`DELETE FROM ingest_jobs WHERE source_ref = $1`, ref)
		var id int64
		if err := db.QueryRow(
			`INSERT INTO ingest_jobs (source_ref, title, artist_name, artist_deezer_id, album_artist_name, album_artist_deezer_id,
			                          album_title, album_deezer_id, duration_ms, status, manual_video_id, known_video_id)
			 VALUES ($1, 'Song', 'Artist', '1', 'Artist', '1', 'Album', '9', 3000, 'pending', NULLIF($2, ''), NULLIF($3, '')) RETURNING id`, ref, manual, known,
		).Scan(&id); err != nil {
			t.Fatal(err)
		}
		job, err := LoadJob(db, ref)
		if err != nil {
			t.Fatal(err)
		}
		ProcessJob(db, job, staging, storage)
		var e, v *string
		db.QueryRow(`SELECT status, error, video_id, attempts FROM ingest_jobs WHERE id = $1`, id).Scan(&status, &e, &v, &attempts)
		if e != nil {
			errText = *e
		}
		if v != nil {
			videoID = *v
		}
		return
	}

	// Видео от админа — без поиска.
	searches, downloads := fakeYouTube(t, "found1")
	if st, e, v, _ := run("deezer:501", "manual1", ""); st != "done" || v != "manual1" || *searches != 0 {
		t.Fatalf("manual: %s %q video %q, searches %d", st, e, v, *searches)
	}
	// Видео админа не скачалось — ошибка, поиска нет.
	if st, _, _, n := run("deezer:502", "goneManual", ""); st != "failed" || *searches != 0 || n != 1 {
		t.Fatalf("gone manual: %s, searches %d, attempts %d", st, *searches, n)
	}
	// Известное видео — сначала оно.
	if st, _, v, _ := run("deezer:503", "", "known1"); st != "done" || v != "known1" || *searches != 0 {
		t.Fatalf("known: %s video %q, searches %d", st, v, *searches)
	}
	// Известное удалили — обычный поиск.
	if st, _, v, _ := run("deezer:504", "", "goneKnown"); st != "done" || v != "found1" || *searches != 1 {
		t.Fatalf("known gone: %s video %q, searches %d, downloads %v", st, v, *searches, *downloads)
	}

	// Не нашлось — failed, попытка засчитана, вид — not_found.
	fakeYouTube(t)
	st, e, _, n := run("deezer:505", "", "")
	if st != "failed" || n != 1 || FailureKind(e) != "not_found" {
		t.Fatalf("not found: %s %q attempts %d", st, e, n)
	}
}

func TestHousekeep_RetriesFailedJobs(t *testing.T) {
	db := openTestDB(t)
	for _, j := range []struct {
		ref      string
		attempts int
		age      string
	}{{"deezer:1", 1, "21 hours"}, {"deezer:2", 3, "21 hours"}, {"deezer:3", 1, "1 hour"}} {
		if _, err := db.Exec(
			`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, error, attempts, updated_at)
			 VALUES ($1, 'S', 'A', 'A', 'Al', 1000, 'failed', 'match: no', $2, now() - $3::interval)`, j.ref, j.attempts, j.age); err != nil {
			t.Fatal(err)
		}
	}
	Housekeep(db, t.TempDir(), 3*24*time.Hour)
	var pending []string
	rows, _ := db.Query(`SELECT source_ref FROM ingest_jobs WHERE status = 'pending' ORDER BY 1`)
	for rows.Next() {
		var r string
		rows.Scan(&r)
		pending = append(pending, r)
	}
	rows.Close()
	if !slices.Equal(pending, []string{"deezer:1"}) {
		t.Fatalf("retried: %v (a day passed and attempts left — only deezer:1)", pending)
	}
}

// Неслушаемое аудио удаляется; задача помнит видео; общий файл живёт, пока
// на него ссылается другой трек; своё, админское и правленое не трогается.
func TestHousekeep_EvictsAudioNobodyPlays(t *testing.T) {
	db := openTestDB(t)
	storage := t.TempDir()
	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Album', '9') RETURNING id`, artistID).Scan(&albumID)
	track := func(dz, key, age string, pinned, edited bool) {
		t.Helper()
		var id string
		if err := db.QueryRow(
			`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id, last_played_at, audio_pinned, edited)
			 VALUES ($1, $2, 'Song', 3000, $3, $4, now() - $5::interval, $6, $7) RETURNING id`,
			albumID, artistID, key, dz, age, pinned, edited).Scan(&id); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(storage, key), []byte("audio"), 0o644)
		db.Exec(`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, result_track_id, video_id)
		         VALUES ($1, 'Song', 'Artist', 'Artist', 'Album', 3000, 'done', $2, $3)`, "deezer:"+dz, id, "vid"+dz)
	}
	track("10", "old.m4a", "5 days", false, false)
	track("11", "fresh.m4a", "1 day", false, false)
	track("12", "shared.m4a", "5 days", false, false)
	track("13", "shared.m4a", "1 day", false, false) // та же запись на другом релизе, слушали вчера
	track("14", "admin.m4a", "30 days", true, false)
	track("15", "edited.m4a", "30 days", false, true)
	track("k99", "uploads/own.m4a", "30 days", false, false)
	os.MkdirAll(filepath.Join(storage, "uploads"), 0o755)
	os.WriteFile(filepath.Join(storage, "uploads/own.m4a"), []byte("audio"), 0o644)

	Housekeep(db, storage, 3*24*time.Hour)

	left := map[string]bool{}
	rows, _ := db.Query(`SELECT deezer_id FROM tracks`)
	for rows.Next() {
		var d string
		rows.Scan(&d)
		left[d] = true
	}
	rows.Close()
	for dz, want := range map[string]bool{"10": false, "11": true, "12": false, "13": true, "14": true, "15": true, "k99": true} {
		if left[dz] != want {
			t.Errorf("track %s kept = %v, want %v", dz, left[dz], want)
		}
	}
	exists := func(key string) bool { _, err := os.Stat(filepath.Join(storage, key)); return err == nil }
	if exists("old.m4a") || !exists("shared.m4a") || !exists("fresh.m4a") || !exists("uploads/own.m4a") {
		t.Errorf("files: old %v shared %v fresh %v own %v", exists("old.m4a"), exists("shared.m4a"), exists("fresh.m4a"), exists("uploads/own.m4a"))
	}
	var status string
	var known, result *string
	db.QueryRow(`SELECT status, known_video_id, result_track_id FROM ingest_jobs WHERE source_ref = 'deezer:10'`).Scan(&status, &known, &result)
	if status != "done" || known == nil || *known != "vid10" || result != nil {
		t.Fatalf("job of an evicted track: %s known %v result %v", status, known, result)
	}
	// Задача удалённого трека не считается мёртвой: по ней трек скачается снова.
	db.Exec(`UPDATE ingest_jobs SET updated_at = now() - interval '30 days' WHERE source_ref = 'deezer:10'`)
	Housekeep(db, storage, 3*24*time.Hour)
	var n int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE source_ref = 'deezer:10'`).Scan(&n)
	if n != 1 {
		t.Fatal("the job of an evicted track was pruned")
	}
}

func TestHousekeep_DropsExpiredDeezerCache(t *testing.T) {
	db := openTestDB(t)
	db.Exec(`DELETE FROM deezer_cache WHERE key LIKE 'test:%'`)
	db.Exec(`INSERT INTO deezer_cache (key, data, expires_at) VALUES
		('test:old', '{}', now() - interval '1 minute'), ('test:fresh', '{}', now() + interval '1 day')`)
	Housekeep(db, t.TempDir(), 3*24*time.Hour)
	var keys []string
	rows, _ := db.Query(`SELECT key FROM deezer_cache WHERE key LIKE 'test:%' ORDER BY key`)
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	if !slices.Equal(keys, []string{"test:fresh"}) {
		t.Errorf("left %v", keys)
	}
}
