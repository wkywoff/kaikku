package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kaikku/internal/deezer"

	"github.com/lib/pq"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping DB-backed test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db ping: %v", err)
	}
	// t.Cleanup runs LIFO — register Close() first so the truncate cleanup
	// (registered second, runs first) still has an open connection to use.
	t.Cleanup(func() { db.Close() })
	t.Cleanup(func() {
		if _, err := db.Exec(`TRUNCATE ingest_jobs, tracks, albums, artists, users, track_edits, track_overrides RESTART IDENTITY CASCADE`); err != nil {
			t.Logf("cleanup truncate failed: %v", err)
		}
	})
	return db
}

func registerTestUser(t *testing.T, router http.Handler) string {
	t.Helper()
	username := fmt.Sprintf("ingesttest-%d", time.Now().UnixNano())
	rec := postJSON(router, "/api/register", fmt.Sprintf(`{"username":%q,"password":"testpass123"}`, username), "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Token
}

func postJSON(router http.Handler, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// artistCalls counts requests for /artist/1 (the artist page cache test).
var artistCalls atomic.Int32

// mockDeezerServer serves a single track (12345, album 9), a two-artist
// compilation (album 77) and a Deezer-style not-found error (track 404) —
// response shapes as Deezer actually returns them (tracklist entries carry
// no "album" object; errors come with HTTP 200).
func mockDeezerServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/track/12345", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":12345,"title":"Test Song","duration":200,"track_position":3,"disk_number":1,"isrc":"TEST00000001","artist":{"id":1,"name":"Test Artist"},"album":{"id":9,"title":"Test Album","cover_xl":"http://example.com/cover.jpg"}}`))
	})
	mux.HandleFunc("/album/9", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":9,"title":"Test Album","cover_xl":"http://example.com/cover.jpg","release_date":"2020-01-02","artist":{"id":1,"name":"Test Artist"}}`))
	})
	mux.HandleFunc("/album/77", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":77,"title":"Compilation","cover_xl":"http://example.com/comp.jpg","release_date":"0000-00-00","artist":{"id":5080,"name":"Various Artists"}}`))
	})
	mux.HandleFunc("/album/77/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"id":501,"title":"Album Track One","duration":180,"track_position":1,"disk_number":1,"artist":{"id":1,"name":"Test Artist"}},
			{"id":502,"title":"Album Track Two","duration":190,"track_position":1,"disk_number":2,"artist":{"id":2,"name":"Other Artist"}}
		],"total":2}`))
	})
	mux.HandleFunc("/album/10", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":10,"title":"Old Single","release_date":"2015-01-01","record_type":"single","explicit_lyrics":false,"artist":{"id":1,"name":"Test Artist"}}`))
	})
	mux.HandleFunc("/album/10/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":12346,"title":"Old Song","duration":180,"track_position":1,"disk_number":1,"artist":{"id":1,"name":"Test Artist"}}],"total":1}`))
	})
	mux.HandleFunc("/album/9/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":12345,"title":"Test Song","duration":200,"track_position":3,"disk_number":1,"artist":{"id":1,"name":"Test Artist"}}],"total":1}`))
	})
	mux.HandleFunc("/artist/1", func(w http.ResponseWriter, r *http.Request) {
		artistCalls.Add(1)
		w.Write([]byte(`{"id":1,"name":"Test Artist","nb_fan":5}`))
	})
	mux.HandleFunc("/artist/1/top", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":888,"title":"Feat Song","duration":101,"artist":{"id":1,"name":"Test Artist"},"album":{"id":9,"title":"Test Album"},
			"contributors":[{"id":1,"name":"Test Artist","role":"Main"},{"id":3,"name":"Feat Artist","role":"Main"}]}],"total":1}`))
	})
	mux.HandleFunc("/track/501", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":501,"title":"Album Track One","duration":180,"track_position":1,"disk_number":1,"artist":{"id":1,"name":"Test Artist"},"album":{"id":77,"title":"Compilation"}}`))
	})
	mux.HandleFunc("/track/888", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":888,"title":"Feat Song","duration":101,"artist":{"id":1,"name":"Test Artist"},"album":{"id":9,"title":"Test Album"},
			"contributors":[{"id":1,"name":"Test Artist","role":"Main"},{"id":3,"name":"Feat Artist","role":"Main"}]}`))
	})
	mux.HandleFunc("/artist/1/related", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":2,"name":"Other Artist"},{"id":3,"name":"Feat Artist"},{"id":4,"name":"Fourth Artist"}],"total":3}`))
	})
	// Артист 2 — «настоящий» артист, которому админ переназначает релизы
	// артиста 1 (handlers_admin_test.go).
	mux.HandleFunc("/artist/2", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":2,"name":"Other Artist","picture_xl":"http://example.com/2.jpg"}`))
	})
	mux.HandleFunc("/artist/2/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"total":0}`))
	})
	mux.HandleFunc("/artist/2/top", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"total":0}`))
	})
	mux.HandleFunc("/artist/2/related", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"total":0}`)) // у артиста нет похожих
	})
	mux.HandleFunc("/artist/1/radio", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":501,"title":"Album Track One","duration":180,"artist":{"id":1,"name":"Test Artist"},"album":{"id":77,"title":"Compilation","cover_xl":"http://example.com/comp.jpg"}}]}`))
	})
	mux.HandleFunc("/artist/1/albums", func(w http.ResponseWriter, r *http.Request) {
		fresh := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
		w.Write([]byte(`{"data":[{"id":9,"title":"Test Album","release_date":"` + fresh + `","record_type":"album"},{"id":10,"title":"Old Single","release_date":"2015-01-01","record_type":"single"}],"total":2}`))
	})
	// Поиск артистов и альбомов: артист 1 — на любой запрос, альбомов нет.
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":1,"name":"Test Artist","nb_fan":5}],"total":1}`))
	})
	mux.HandleFunc("/search/album", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"total":0}`))
	})
	// Поиск (импорт плейлиста): «Test Song» находится, остальное — нет.
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(strings.ToLower(r.URL.Query().Get("q")), "test song") {
			w.Write([]byte(`{"data":[],"total":0}`))
			return
		}
		w.Write([]byte(`{"data":[
			{"id":777,"title":"Test Song (Live)","duration":260,"artist":{"id":1,"name":"Test Artist"},"album":{"id":9,"title":"Test Album"}},
			{"id":12345,"title":"Test Song","duration":200,"artist":{"id":1,"name":"Test Artist"},"album":{"id":9,"title":"Test Album"}}
		],"total":2}`))
	})
	mux.HandleFunc("/track/404", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	})
	return httptest.NewServer(mux)
}

func newTestRouter(t *testing.T) (*sql.DB, http.Handler, string) {
	t.Helper()
	db, router, token, _ := newTestServer(t)
	return db, router, token
}

// newTestServer — то же, что newTestRouter, и сам Server: его поля
// (StorageDir и др.) читаются на каждом запросе, тест может их поменять.
func newTestServer(t *testing.T) (*sql.DB, http.Handler, string, *Server) {
	t.Helper()
	db := openTestDB(t)
	deezerSrv := mockDeezerServer(t)
	t.Cleanup(deezerSrv.Close)
	srv := &Server{
		DB:        db,
		JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"),
		Deezer:    deezer.NewClientWithBaseURL(deezerSrv.URL),
		// Фоновое скачивание — сразу, иначе оно гонялось бы с закрытием тестовой БД.
		Background: func(f func()) { f() },
	}
	router := NewRouter(srv)
	return db, router, registerTestUser(t, router), srv
}

// jobOf returns the ingest job of a Deezer track.
func jobOf(t *testing.T, db *sql.DB, deezerID string) (id int64, status string) {
	t.Helper()
	if err := db.QueryRow(`SELECT id, status FROM ingest_jobs WHERE source_ref = $1`, "deezer:"+deezerID).Scan(&id, &status); err != nil {
		t.Fatalf("job of deezer:%s: %v", deezerID, err)
	}
	return id, status
}

// На скачивание ставит лайк (и сохранение альбома, плейлист, воспроизведение);
// в тестах фоновая работа идёт сразу (Server.Background).
func TestLike_QueuesDownloadOnceWithAlbumMetadata(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	first, status := jobOf(t, db, "12345")
	if status != "pending" {
		t.Fatalf("status = %s, want pending", status)
	}
	call(t, router, "DELETE", "/api/me/liked/12345", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	var count int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).Scan(&count)
	if again, _ := jobOf(t, db, "12345"); count != 1 || again != first {
		t.Errorf("a second like must reuse the job: %d jobs", count)
	}

	var artist, artistID, albumArtist, albumArtistID, albumTitle, albumID, coverURL, release string
	var trackNo, discNo int
	err := db.QueryRow(
		`SELECT artist_name, artist_deezer_id, album_artist_name, album_artist_deezer_id, album_title,
		        album_deezer_id, cover_url, release_date::text, track_no, disc_no
		   FROM ingest_jobs WHERE source_ref = 'deezer:12345'`,
	).Scan(&artist, &artistID, &albumArtist, &albumArtistID, &albumTitle, &albumID, &coverURL, &release, &trackNo, &discNo)
	if err != nil {
		t.Fatalf("metadata query: %v", err)
	}
	if artist != "Test Artist" || artistID != "1" || albumArtist != "Test Artist" || albumArtistID != "1" ||
		albumTitle != "Test Album" || albumID != "9" || coverURL != "http://example.com/cover.jpg" ||
		release != "2020-01-02" || trackNo != 3 || discNo != 1 {
		t.Errorf("unexpected job metadata: %s/%s %s/%s %q/%s %s %s %d/%d",
			artist, artistID, albumArtist, albumArtistID, albumTitle, albumID, coverURL, release, trackNo, discNo)
	}
}

func TestSaveAlbum_CompilationKeepsAlbumArtist(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/albums/77", token, "", http.StatusNoContent)

	// Треклист альбома не содержит данных альбома — они должны прийти из
	// /album/{id}, а альбомный исполнитель не должен подменяться трековым.
	rows, err := db.Query(
		`SELECT artist_name, album_artist_name, album_title, album_deezer_id, cover_url, release_date IS NULL, disc_no
		   FROM ingest_jobs WHERE source_ref IN ('deezer:501', 'deezer:502') ORDER BY source_ref`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	wantArtists := []string{"Test Artist", "Other Artist"}
	i := 0
	for rows.Next() {
		var artist, albumArtist, albumTitle, albumID, cover string
		var noDate bool
		var disc int
		rows.Scan(&artist, &albumArtist, &albumTitle, &albumID, &cover, &noDate, &disc)
		if artist != wantArtists[i] || albumArtist != "Various Artists" || albumTitle != "Compilation" ||
			albumID != "77" || cover != "http://example.com/comp.jpg" || !noDate || disc != i+1 {
			t.Errorf("row %d: %s / %s / %q / %s / %s / nodate=%v / disc=%d", i, artist, albumArtist, albumTitle, albumID, cover, noDate, disc)
		}
		i++
	}
	if i != 2 {
		t.Fatalf("got %d jobs, want 2", i)
	}

	// Сохранили заново — дублей нет.
	call(t, router, "DELETE", "/api/me/albums/77", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/77", token, "", http.StatusNoContent)
	var count int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs`).Scan(&count)
	if count != 2 {
		t.Errorf("re-saving the album: %d jobs, want 2", count)
	}
}

func TestDownload_FailedJobIsRequeued(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	job, _ := jobOf(t, db, "12345")
	db.Exec(`UPDATE ingest_jobs SET status = 'failed', error = 'match: nothing' WHERE id = $1`, job)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	var status string
	var errMsg sql.NullString
	db.QueryRow(`SELECT status, error FROM ingest_jobs WHERE id = $1`, job).Scan(&status, &errMsg)
	if status != "pending" || errMsg.Valid {
		t.Errorf("after requeue: status=%s error=%v", status, errMsg)
	}
}

func TestDownload_DeletedTrackIsRequeued(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	job, _ := jobOf(t, db, "12345")

	var artistID, albumID, trackID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.QueryRow(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, 'Test Song', 200000, 'x.m4a', '12345') RETURNING id`, albumID).Scan(&trackID)
	db.Exec(`UPDATE ingest_jobs SET status = 'done', result_track_id = $1 WHERE id = $2`, trackID, job)

	// Раньше падало на FK из ingest_jobs.result_track_id.
	if _, err := db.Exec(`DELETE FROM tracks WHERE id = $1`, trackID); err != nil {
		t.Fatalf("deleting an ingested track: %v", err)
	}

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	if again, status := jobOf(t, db, "12345"); again != job || status != "pending" {
		t.Fatalf("want job %d re-queued after its track was deleted, got %d %s", job, again, status)
	}
}

func TestLike_BadInput(t *testing.T) {
	_, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/abc", token, "", http.StatusBadRequest)
	call(t, router, "PUT", "/api/me/liked/404", token, "", http.StatusNotFound)
}

func TestHandleIngestLookup(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	job, _ := jobOf(t, db, "12345")
	db.Exec(`UPDATE ingest_jobs SET status = 'failed' WHERE id = $1`, job)

	var got map[string]struct {
		JobID     int64  `json:"job_id"`
		JobStatus string `json:"job_status"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/ingest/lookup?ids=12345,999", token, "", http.StatusOK), &got)
	if e, ok := got["12345"]; !ok || e.JobID != job || e.JobStatus != "failed" {
		t.Errorf("12345: %+v", got)
	}
	if _, ok := got["999"]; ok {
		t.Errorf("never-queued id must be absent: %+v", got)
	}
	call(t, router, "GET", "/api/ingest/lookup?ids=1,x", token, "", http.StatusBadRequest)
}

func TestDownload_OtherArtistsForSearch(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/888", token, "", http.StatusNoContent)
	job, _ := jobOf(t, db, "888")
	var others []string
	db.QueryRow(`SELECT other_artists FROM ingest_jobs WHERE id = $1`, job).Scan(pq.Array(&others))
	if !slices.Equal(others, []string{"Feat Artist"}) {
		t.Fatalf("other_artists = %q, want [Feat Artist]", others)
	}

	// Job из времён до миграции 0008: при перезапуске упавшего фиты дозаполняются.
	db.Exec(`UPDATE ingest_jobs SET status = 'failed', other_artists = '{}' WHERE id = $1`, job)
	call(t, router, "PUT", "/api/me/liked/888", token, "", http.StatusNoContent)
	others = nil
	var status string
	db.QueryRow(`SELECT status, other_artists FROM ingest_jobs WHERE id = $1`, job).Scan(&status, pq.Array(&others))
	if status != "pending" || !slices.Equal(others, []string{"Feat Artist"}) {
		t.Errorf("after requeue: status %s, other_artists = %q, want pending [Feat Artist]", status, others)
	}
}

// У артиста без похожих — пустой список, а не null (на null падала страница артиста).
func TestRelatedArtistsEmptyIsArray(t *testing.T) {
	_, router, token := newTestRouter(t)
	body := call(t, router, "GET", "/api/discover/artists/2/related", token, "", http.StatusOK)
	if !strings.Contains(string(body), `"artists":[]`) {
		t.Fatalf("want an empty array, got %s", body)
	}
}
