package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"kaikku/internal/ytdlp"
)

const testCookies = "# Netscape HTTP Cookie File\n" +
	".youtube.com\tTRUE\t/\tTRUE\t1830000000\tSAPISID\tyt-secret-1\n" +
	".youtube.com\tTRUE\t/\tFALSE\t0\tPREF\tf6=40000000\n" +
	".google.com\tTRUE\t/\tTRUE\t1830000000\tSID\tgoogle-secret\n"

func TestYouTubeCookies_UploadCheckAndRemove(t *testing.T) {
	db, router, admin, _ := newTestServer(t)
	ytdlp.SetCookiesFile(filepath.Join(t.TempDir(), "youtube-cookies.txt"))
	t.Cleanup(func() { ytdlp.SetCookiesFile("") })
	var checked []string
	var checkErr error
	old := youtubeCheck
	youtubeCheck = func(_ context.Context, videoID string) error {
		checked = append(checked, videoID)
		return checkErr
	}
	t.Cleanup(func() { youtubeCheck = old })
	if _, err := db.Exec(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, error, attempts, video_id)
		 VALUES ('deezer:12348', 'Song', 'Artist', 'Artist', 'Album', 200000, 'failed',
		         'blocked: download: yt-dlp download: exit status 1 (output: Sign in to confirm you’re not a bot)', 0, 'refusedVid1'),
		        ('deezer:12349', 'Other', 'Artist', 'Artist', 'Album', 200000, 'done', NULL, 0, 'doneVideo01')`); err != nil {
		t.Fatal(err)
	}
	jobStatus := func() (status string) {
		db.QueryRow(`SELECT status FROM ingest_jobs WHERE source_ref = 'deezer:12348'`).Scan(&status)
		return
	}
	user := invitedUser(t, router, admin, "cookies-user")
	call(t, router, "PUT", "/api/admin/youtube/cookies", user, testCookies, http.StatusForbidden)

	for body, reason := range map[string]string{
		`[{"domain": ".youtube.com", "name": "SID"}]`: "json",
		"hello": "format",
		".google.com\tTRUE\t/\tTRUE\t0\tSID\tx\n": "no_youtube",
	} {
		var res struct{ Reason string }
		json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, body, http.StatusBadRequest), &res)
		if res.Reason != reason {
			t.Errorf("%q: reason %q, want %q", body, res.Reason, reason)
		}
	}

	type upload struct {
		Cookies  ytdlp.CookiesInfo
		Check    string
		Requeued int
	}
	var up upload
	checkErr = fmt.Errorf("yt-dlp: %w", ytdlp.ErrBlocked)
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "blocked" || up.Cookies.Count != 2 || !up.Cookies.SignedIn || len(checked) != 1 || checked[0] != "refusedVid1" {
		t.Fatalf("still refused: %+v, checked %v", up, checked)
	}
	if st := jobStatus(); st != "failed" {
		t.Fatalf("refused job: %s", st)
	}

	checkErr = nil
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" || up.Requeued != 1 {
		t.Fatalf("lets in: %+v", up)
	}
	if st := jobStatus(); st != "pending" {
		t.Fatalf("refused job after the check: %s", st)
	}

	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" || up.Requeued != 0 || checked[len(checked)-1] != "doneVideo01" {
		t.Fatalf("check without refused jobs: %+v, checked %v", up, checked)
	}

	body := call(t, router, "GET", "/api/admin/missing", admin, "", http.StatusOK)
	if strings.Contains(string(body), "secret") {
		t.Fatalf("cookie values leaked: %s", body)
	}
	var list struct{ Cookies *ytdlp.CookiesInfo }
	json.Unmarshal(body, &list)
	if list.Cookies == nil || list.Cookies.Count != 2 {
		t.Fatalf("missing: cookies %+v", list.Cookies)
	}

	call(t, router, "DELETE", "/api/admin/youtube/cookies", admin, "", http.StatusNoContent)
	list.Cookies = nil
	json.Unmarshal(call(t, router, "GET", "/api/admin/missing", admin, "", http.StatusOK), &list)
	if list.Cookies != nil {
		t.Fatalf("after removing: %+v", list.Cookies)
	}

	db.Exec(`DELETE FROM ingest_jobs`)
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "skipped" {
		t.Fatalf("nothing to check on: %+v", up)
	}
}

func TestYouTubeCookies_SignInSongs(t *testing.T) {
	db, router, admin, _ := newTestServer(t)
	ytdlp.SetCookiesFile(filepath.Join(t.TempDir(), "youtube-cookies.txt"))
	t.Cleanup(func() { ytdlp.SetCookiesFile("") })
	ageConfirmed := false
	old := youtubeCheck
	youtubeCheck = func(_ context.Context, videoID string) error {
		if videoID == "ageVideo001" && !ageConfirmed {
			return fmt.Errorf("yt-dlp: Sorry, this content is age-restricted. To view this video, "+
				"please provide more info so we can be sure you're an adult: %w", ytdlp.ErrSignIn)
		}
		return nil
	}
	t.Cleanup(func() { youtubeCheck = old })
	if _, err := db.Exec(
		`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, error, attempts, video_id)
		 VALUES ('deezer:12350', 'Adult', 'Artist', 'Artist', 'Album', 200000, 'failed',
		         'signin: download: yt-dlp download: exit status 1 (output: Sign in to confirm your age)', 3, 'ageVideo001'),
		        ('deezer:12351', 'Other', 'Artist', 'Artist', 'Album', 200000, 'done', NULL, 0, 'doneVideo01')`); err != nil {
		t.Fatal(err)
	}
	job := func() (status string, attempts int) {
		db.QueryRow(`SELECT status, attempts FROM ingest_jobs WHERE source_ref = 'deezer:12350'`).Scan(&status, &attempts)
		return
	}
	type upload struct {
		Check, Signin string
		Requeued      int
	}

	var up upload
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" || up.Signin != "age" || up.Requeued != 0 {
		t.Fatalf("unconfirmed account: %+v", up)
	}
	if st, _ := job(); st != "failed" {
		t.Fatalf("the 18+ song after unconfirmed cookies: %s", st)
	}

	ageConfirmed = true
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" || up.Signin != "ok" || up.Requeued != 1 {
		t.Fatalf("confirmed account: %+v", up)
	}
	if st, n := job(); st != "pending" || n != 0 {
		t.Fatalf("the 18+ song after good cookies: %s, attempts %d", st, n)
	}
}
