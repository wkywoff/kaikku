package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"testing"
)

func TestMissing_BlockedByYouTube(t *testing.T) {
	db, router, admin, _ := newTestServer(t)
	var list struct {
		Tracks         []missingTrack
		Blocked, Proxy bool
	}
	json.Unmarshal(call(t, router, "GET", "/api/admin/missing", admin, "", http.StatusOK), &list)
	if list.Blocked || list.Proxy || len(list.Tracks) != 0 {
		t.Fatalf("empty list: %+v", list)
	}
	if _, err := db.Exec(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, error, attempts)
		 VALUES ('deezer:12347', 'Song', 'Artist', 'Artist', 'Album', 200000, 'failed',
		         'blocked: download: yt-dlp download: exit status 1 (output: ERROR: [youtube] x: Sign in to confirm you’re not a bot)', 0)`); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(call(t, router, "GET", "/api/admin/missing", admin, "", http.StatusOK), &list)
	if !list.Blocked || len(list.Tracks) != 1 || list.Tracks[0].Kind != "blocked" {
		t.Fatalf("blocked: %+v", list)
	}
}

func TestMissing_ListVideoAndUpload(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	if _, err := db.Exec(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, artist_deezer_id, album_artist_name, album_artist_deezer_id,
		                          album_title, album_deezer_id, duration_ms, status, error, attempts)
		 VALUES ('deezer:12345', 'Test Song', 'Test Artist', '1', 'Test Artist', '1', 'Test Album', '9', 200000, 'failed',
		         'match: "Test Artist Test Song": youtube search returned no results', 2),
		        ('deezer:12346', 'Gave Up', 'Test Artist', '1', 'Test Artist', '1', 'Test Album', '9', 200000, 'failed',
		         'match: "Test Artist Gave Up": youtube search returned no results', 3)`); err != nil {
		t.Fatal(err)
	}
	user := invitedUser(t, router, admin, "missing-user")
	call(t, router, "GET", "/api/admin/missing", user, "", http.StatusForbidden)

	var list struct{ Tracks []missingTrack }
	json.Unmarshal(call(t, router, "GET", "/api/admin/missing", admin, "", http.StatusOK), &list)
	if len(list.Tracks) != 1 || list.Tracks[0].DeezerID != "12345" || list.Tracks[0].Kind != "not_found" || list.Tracks[0].Attempts != 2 {
		t.Fatalf("missing: %+v", list.Tracks)
	}

	call(t, router, "PUT", "/api/tracks/deezer/12345/video", admin, `{"url":"https://example.com/x"}`, http.StatusBadRequest)
	call(t, router, "PUT", "/api/tracks/deezer/12345/video", admin, `{"url":"https://youtu.be/dQw4w9WgXcQ?si=x"}`, http.StatusAccepted)
	var status, manual string
	var attempts int
	var replace bool
	db.QueryRow(`SELECT status, manual_video_id, attempts, replace_audio FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).
		Scan(&status, &manual, &attempts, &replace)
	if status != "pending" || manual != "dQw4w9WgXcQ" || attempts != 0 || replace {
		t.Fatalf("after the link: %s %q attempts %d replace %v", status, manual, attempts, replace)
	}
	call(t, router, "PUT", "/api/tracks/deezer/12345/video", admin, `{"url":"dQw4w9WgXcQ"}`, http.StatusConflict)

	var jobID int64
	db.QueryRow(`UPDATE ingest_jobs SET status = 'failed' WHERE source_ref = 'deezer:12345' RETURNING id`).Scan(&jobID)
	call(t, router, "POST", "/api/admin/missing/"+strconv.FormatInt(jobID, 10)+"/retry", admin, "", http.StatusAccepted)
	db.QueryRow(`SELECT status, manual_video_id FROM ingest_jobs WHERE id = $1`, jobID).Scan(&status, &manual)
	if status != "pending" || manual != "dQw4w9WgXcQ" {
		t.Fatalf("after retry: %s %q", status, manual)
	}

	db.Exec(`UPDATE ingest_jobs SET status = 'failed' WHERE id = $1`, jobID)
	if rec := adminRequest(router, "PUT", "/api/tracks/deezer/12345/audio", admin, wavTone(), "audio/wav"); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var pinned bool
	var result string
	db.QueryRow(`SELECT t.audio_pinned, j.status, j.result_track_id FROM tracks t JOIN ingest_jobs j ON j.source_ref = 'deezer:' || t.deezer_id
	             WHERE t.deezer_id = '12345'`).Scan(&pinned, &status, &result)
	if !pinned || status != "done" || result == "" {
		t.Fatalf("after upload: pinned %v, job %s, result %q", pinned, status, result)
	}
}
