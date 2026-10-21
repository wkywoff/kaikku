package ingest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Трек, скачанный API во время потокового воспроизведения, воркер берёт как
// есть: без поиска и скачивания (yt-dlp в PATH нет — вызов провалил бы job).
func TestProcessJob_UsesPrefetchedAudio(t *testing.T) {
	db := openTestDB(t)
	bin := t.TempDir()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed", tool)
		}
		os.Symlink(path, filepath.Join(bin, tool))
	}
	staging, storage := t.TempDir(), t.TempDir()

	var jobID int64
	err := db.QueryRow(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, artist_deezer_id, album_artist_name, album_artist_deezer_id,
		                          album_title, album_deezer_id, duration_ms, status, video_id)
		 VALUES ('deezer:777', 'Song', 'Artist', '1', 'Artist', '1', 'Album', '9', 3000, 'processing', 'vid1') RETURNING id`,
	).Scan(&jobID)
	if err != nil {
		t.Fatal(err)
	}
	// DASH-MP4 с AAC — как формат 140 с YouTube.
	prefetch := PrefetchPath(staging, jobID)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:a", "aac", "-movflags", "+dash+global_sidx", "-f", "mp4", prefetch).CombinedOutput(); err != nil {
		t.Fatalf("making a DASH file: %v %s", err, out)
	}
	t.Setenv("PATH", bin)

	ProcessJob(db, &Job{ID: jobID, SourceRef: "deezer:777", Title: "Song", ArtistName: "Artist", ArtistDeezerID: str("1"),
		AlbumArtistName: "Artist", AlbumArtistDeezerID: str("1"), AlbumTitle: "Album", AlbumDeezerID: str("9"), DurationMs: 3000}, staging, storage)

	var status string
	var errMsg, storageKey, videoID *string
	db.QueryRow(`SELECT j.status, j.error, t.storage_key, j.video_id FROM ingest_jobs j
	             LEFT JOIN tracks t ON t.id = j.result_track_id WHERE j.id = $1`, jobID).
		Scan(&status, &errMsg, &storageKey, &videoID)
	if status != "done" || storageKey == nil {
		t.Fatalf("job status %q, error %v", status, errMsg)
	}
	if videoID == nil || *videoID != "vid1" {
		t.Errorf("video_id = %v, want the one the API recorded", videoID)
	}
	if _, err := os.Stat(filepath.Join(storage, *storageKey)); err != nil {
		t.Errorf("library file: %v", err)
	}
	if _, err := os.Stat(prefetch); !os.IsNotExist(err) {
		t.Errorf("prefetched file left in staging: %v", err)
	}
}

// Та же запись на другом релизе (ISRC) уже в медиатеке: воркер заводит трек
// на её файл, без поиска и скачивания (yt-dlp в PATH нет — вызов провалил бы job).
func TestProcessJob_SharesFileOfSameRecording(t *testing.T) {
	db := openTestDB(t)
	t.Setenv("PATH", t.TempDir())
	storage := t.TempDir()

	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Album', '9') RETURNING id`, artistID).Scan(&albumID)
	if _, err := db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, gain_db, deezer_id, isrc)
	                      VALUES ($1, $2, 'Song', 3000, 'shared.m4a', -2.5, '100', 'ISRC00000001')`, albumID, artistID); err != nil {
		t.Fatal(err)
	}
	var jobID int64
	if err := db.QueryRow(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, artist_deezer_id, album_artist_name, album_artist_deezer_id,
		                          album_title, album_deezer_id, duration_ms, status, isrc)
		 VALUES ('deezer:200', 'Song', 'Artist', '1', 'Artist', '1', 'Album (Deluxe)', '10', 3000, 'processing', 'ISRC00000001') RETURNING id`,
	).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	ProcessJob(db, &Job{ID: jobID, SourceRef: "deezer:200", Title: "Song", ArtistName: "Artist", ArtistDeezerID: str("1"),
		AlbumArtistName: "Artist", AlbumArtistDeezerID: str("1"), AlbumTitle: "Album (Deluxe)", AlbumDeezerID: str("10"),
		DurationMs: 3000, ISRC: str("ISRC00000001")}, t.TempDir(), storage)

	var status, key, isrc string
	var errMsg *string
	var gain float64
	db.QueryRow(`SELECT j.status, j.error, COALESCE(t.storage_key, ''), COALESCE(t.gain_db, 0), COALESCE(t.isrc, '')
	               FROM ingest_jobs j LEFT JOIN tracks t ON t.id = j.result_track_id WHERE j.id = $1`, jobID).
		Scan(&status, &errMsg, &key, &gain, &isrc)
	if status != "done" || key != "shared.m4a" || gain != -2.5 || isrc != "ISRC00000001" {
		t.Fatalf("status %q (error %v), storage %q, gain %v, isrc %q", status, errMsg, key, gain, isrc)
	}
}

// Перекачка (админ): трек уже в медиатеке — файл заменяется в той же строке
// трека (лайки и плейлисты при нём) и у той же записи на другом релизе,
// которая делила с ним файл; старый удаляется, чужие файлы не трогаются.
func TestProcessJob_ReplaceAudioKeepsTheTrack(t *testing.T) {
	db := openTestDB(t)
	staging, storage := t.TempDir(), t.TempDir()

	var artistID, albumID, trackID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.QueryRow(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Song', 3000, 'old.m4a', '777') RETURNING id`,
		albumID, artistID).Scan(&trackID)
	var deluxeID string
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Album (Deluxe)', '10') RETURNING id`, artistID).Scan(&deluxeID)
	db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id, audio_pinned)
	         VALUES ($1, $2, 'Song', 3000, 'old.m4a', '778', true), ($1, $2, 'Other Song', 3000, 'other.m4a', '779', false)`,
		deluxeID, artistID)
	os.WriteFile(filepath.Join(storage, "old.m4a"), []byte("wrong song"), 0o644)
	os.WriteFile(filepath.Join(storage, "other.m4a"), []byte("other song"), 0o644)
	var jobID int64
	db.QueryRow(`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, replace_audio)
	             VALUES ('deezer:777', 'Song', 'Artist', 'Artist', 'Album', 3000, 'processing', true) RETURNING id`).Scan(&jobID)
	prefetchAudio(t, staging, jobID)

	ProcessJob(db, &Job{ID: jobID, SourceRef: "deezer:777", Title: "Song", ArtistName: "Artist", ArtistDeezerID: str("1"),
		AlbumArtistName: "Artist", AlbumArtistDeezerID: str("1"), AlbumTitle: "Album", AlbumDeezerID: str("9"), DurationMs: 3000,
		ReplaceAudio: true}, staging, storage)

	var status, result, key string
	var replace bool
	db.QueryRow(`SELECT j.status, j.result_track_id, j.replace_audio, t.storage_key FROM ingest_jobs j JOIN tracks t ON t.deezer_id = '777'
	             WHERE j.id = $1`, jobID).Scan(&status, &result, &replace, &key)
	if status != "done" || result != trackID || replace || key == "old.m4a" {
		t.Fatalf("status %q, result %q (track %q), replace %v, key %q", status, result, trackID, replace, key)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM tracks`).Scan(&n)
	if n != 3 {
		t.Errorf("%d tracks, want the same three", n)
	}
	var deluxeKey, otherKey string
	var pinned bool
	db.QueryRow(`SELECT storage_key, audio_pinned FROM tracks WHERE deezer_id = '778'`).Scan(&deluxeKey, &pinned)
	db.QueryRow(`SELECT storage_key FROM tracks WHERE deezer_id = '779'`).Scan(&otherKey)
	if deluxeKey != key || pinned {
		t.Errorf("same recording on the deluxe: key %q pinned %v, want %q unpinned", deluxeKey, pinned, key)
	}
	if otherKey != "other.m4a" {
		t.Errorf("another song's file changed: %q", otherKey)
	}
	if _, err := os.Stat(filepath.Join(storage, "other.m4a")); err != nil {
		t.Errorf("another song's file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage, "old.m4a")); !os.IsNotExist(err) {
		t.Errorf("old file left: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage, key)); err != nil {
		t.Errorf("new file: %v", err)
	}
}

// prefetchAudio puts a short DASH file where the API leaves a streamed
// download for the job, so ProcessJob skips YouTube, and leaves only
// ffmpeg/ffprobe in PATH.
func prefetchAudio(t *testing.T, staging string, jobID int64) {
	t.Helper()
	bin := t.TempDir()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed", tool)
		}
		os.Symlink(path, filepath.Join(bin, tool))
	}
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:a", "aac", "-movflags", "+dash+global_sidx", "-f", "mp4", PrefetchPath(staging, jobID)).CombinedOutput(); err != nil {
		t.Fatalf("making a DASH file: %v %s", err, out)
	}
	t.Setenv("PATH", bin)
}
