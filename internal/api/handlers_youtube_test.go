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

	// Не те файлы — страница по reason объясняет, что не так.
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

	// YouTube всё равно не пускает: куки сохранены, задача ждёт.
	type upload struct {
		Cookies ytdlp.CookiesInfo
		Check   string
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

	// Пускает — отказанная задача снова в очереди.
	checkErr = nil
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" {
		t.Fatalf("lets in: %+v", up)
	}
	if st := jobStatus(); st != "pending" {
		t.Fatalf("refused job after the check: %s", st)
	}

	// Отказанных нет — проверяется видео, которое уже скачивалось.
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "ok" || checked[len(checked)-1] != "doneVideo01" {
		t.Fatalf("check without refused jobs: %+v, checked %v", up, checked)
	}

	// «Не нашлось» показывает, что куки есть; сами значения сервер не отдаёт.
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

	// Не на чем проверить (новая установка): просто сохранены.
	db.Exec(`DELETE FROM ingest_jobs`)
	json.Unmarshal(call(t, router, "PUT", "/api/admin/youtube/cookies", admin, testCookies, http.StatusOK), &up)
	if up.Check != "skipped" {
		t.Fatalf("nothing to check on: %+v", up)
	}
}
