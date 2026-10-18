package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/ingest"
	"kaikku/internal/media"
	"kaikku/internal/ytdlp"
)

// dashFile builds a file shaped like YouTube's format 140 (ftyp, moov,
// sidx listing the segments, moof+mdat segments) — what yt-dlp streams.
// headerLen is where the first segment starts.
func dashFile(segments ...int) (file []byte, headerLen int) {
	box := func(typ string, payload []byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
		return append(append(b, typ...), payload...)
	}
	sidx := []byte{0, 0, 0, 0}
	sidx = binary.BigEndian.AppendUint32(sidx, 1)
	sidx = binary.BigEndian.AppendUint32(sidx, 44100)
	sidx = binary.BigEndian.AppendUint64(sidx, 0) // earliest_presentation_time + first_offset
	sidx = binary.BigEndian.AppendUint32(sidx, uint32(len(segments)))
	for _, n := range segments {
		sidx = binary.BigEndian.AppendUint32(sidx, uint32(n))
		sidx = binary.BigEndian.AppendUint64(sidx, 0)
	}
	var f bytes.Buffer
	f.Write(box("ftyp", []byte("dash\x00\x00\x00\x00iso6mp41")))
	f.Write(box("moov", make([]byte, 100)))
	f.Write(box("sidx", sidx))
	headerLen = f.Len()
	for i, n := range segments {
		f.Write(box("moof", make([]byte, 16)))
		f.Write(box("mdat", bytes.Repeat([]byte{byte('a' + i)}, n-24-8)))
	}
	return f.Bytes(), headerLen
}

// fakeSource is a StreamSource whose audio the test feeds by hand, to see
// what the listener gets while the "download" is still going.
type fakeSource struct {
	calls atomic.Int32
	want  chan ytdlp.Want
	pipe  *io.PipeWriter
	err   error
	ready chan struct{}
}

func newFakeSource() *fakeSource {
	return &fakeSource{want: make(chan ytdlp.Want, 10), ready: make(chan struct{})}
}

func (s *fakeSource) open(ctx context.Context, want ytdlp.Want) (string, io.ReadCloser, error) {
	s.calls.Add(1)
	s.want <- want
	if s.err != nil {
		return "", nil, s.err
	}
	r, w := io.Pipe()
	s.pipe = w
	close(s.ready)
	return "vid1", r, nil
}

func newStreamTestServer(t *testing.T) (*Server, *httptest.Server, string, *fakeSource) {
	t.Helper()
	db := openTestDB(t)
	deezerSrv := mockDeezerServer(t)
	t.Cleanup(deezerSrv.Close)
	src := newFakeSource()
	t.Cleanup(func() { // скачивание, которое тест так и не докормил
		select {
		case <-src.ready:
			src.pipe.CloseWithError(errors.New("test is over"))
		default:
		}
	})
	staging := t.TempDir()
	hub, err := NewStreamHub(t.TempDir()+"/stream", staging)
	if err != nil {
		t.Fatal(err)
	}
	hub.Source = src.open
	srv := &Server{
		DB:          db,
		JWTSecret:   []byte("test-secret-at-least-32-bytes-long!!"),
		MediaSigner: &media.Signer{Secret: "s3cret", TTL: time.Minute},
		Deezer:      deezer.NewClientWithBaseURL(deezerSrv.URL),
		Streams:     hub,
	}
	router := NewRouter(srv)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return srv, ts, registerTestUser(t, router), src
}

type streamURLResp struct {
	Status  string  `json:"status"`
	JobID   int64   `json:"job_id"`
	TrackID string  `json:"track_id"`
	URL     string  `json:"url"`
	GainDB  float64 `json:"gain_db"`
}

func getStreamURL(t *testing.T, ts *httptest.Server, token, deezerID string) (int, streamURLResp) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream/deezer/"+deezerID+"/url", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var r streamURLResp
	json.NewDecoder(res.Body).Decode(&r)
	return res.StatusCode, r
}

func getAudio(t *testing.T, url, rangeHeader string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDeezerStream_PlaysWhileDownloadingThenHandsOffToWorker(t *testing.T) {
	srv, ts, token, src := newStreamTestServer(t)
	file, headerLen := dashFile(5000, 7000, 6000)

	code, link := getStreamURL(t, ts, token, "12345")
	if code != http.StatusAccepted || link.Status != "queued" || link.JobID == 0 || link.TrackID != "" {
		t.Fatalf("stream url: %d %+v", code, link)
	}
	if !strings.HasPrefix(link.URL, "/api/stream/deezer/12345?st=") {
		t.Fatalf("stream url = %q", link.URL)
	}
	var held bool
	srv.DB.QueryRow(`SELECT hold_until > now() FROM ingest_jobs WHERE id = $1`, link.JobID).Scan(&held)
	if !held {
		t.Fatal("job is not held from the worker while the client starts the stream")
	}
	// Воркер придержанный job не берёт.
	if job, err := ingest.ClaimNextJob(srv.DB); err != nil || job != nil {
		t.Fatalf("ClaimNextJob on a held job = %+v, %v; want nothing", job, err)
	}

	resCh := make(chan *http.Response)
	go func() { resCh <- getAudio(t, ts.URL+link.URL, "") }()
	want := <-src.want
	if want.Title != "Test Song" || want.Artist != "Test Artist" || want.Album != "Test Album" || want.DurationMs != 200000 {
		t.Errorf("matching data = %+v", want)
	}
	<-src.ready
	// Ответ уходит, как только из заголовка файла известен размер, — скачано
	// лишь начало.
	go src.pipe.Write(file[:headerLen+5000])
	res := <-resCh
	if res.StatusCode != http.StatusOK || res.ContentLength != int64(len(file)) ||
		res.Header.Get("Content-Type") != "audio/mp4" || res.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("response: %d, length %d, headers %v", res.StatusCode, res.ContentLength, res.Header)
	}
	first := make([]byte, headerLen+5000)
	if _, err := io.ReadFull(res.Body, first); err != nil || !bytes.Equal(first, file[:headerLen+5000]) {
		t.Fatalf("first segment before the download finished: %v", err)
	}

	// Перемотка в уже скачанное — 206, пока трек ещё качается.
	part := getAudio(t, ts.URL+link.URL, "bytes=100-199")
	body, _ := io.ReadAll(part.Body)
	if part.StatusCode != http.StatusPartialContent || !bytes.Equal(body, file[100:200]) ||
		part.Header.Get("Content-Range") != fmt.Sprintf("bytes 100-199/%d", len(file)) {
		t.Fatalf("range: %d %q, %d bytes", part.StatusCode, part.Header.Get("Content-Range"), len(body))
	}

	// Перемотка вперёд, в ещё не скачанное, ждёт свои байты.
	tailDone := make(chan []byte)
	go func() {
		tail := getAudio(t, ts.URL+link.URL, "bytes=-10")
		b, _ := io.ReadAll(tail.Body)
		tailDone <- b
	}()

	src.pipe.Write(file[headerLen+5000:])
	src.pipe.Close()

	rest, err := io.ReadAll(res.Body)
	if err != nil || !bytes.Equal(append(first, rest...), file) {
		t.Fatalf("full body: %v, %d bytes", err, len(first)+len(rest))
	}
	if tail := <-tailDone; !bytes.Equal(tail, file[len(file)-10:]) {
		t.Errorf("tail range = %q", tail)
	}

	// Скачанное — воркеру: файл в staging, video_id записан, hold снят.
	prefetch := ingest.PrefetchPath(srv.Streams.StagingDir, link.JobID)
	waitFor(t, "hand-off to the worker", func() bool {
		var released bool
		srv.DB.QueryRow(`SELECT hold_until IS NULL FROM ingest_jobs WHERE id = $1`, link.JobID).Scan(&released)
		return released
	})
	if got, err := os.ReadFile(prefetch); err != nil || !bytes.Equal(got, file) {
		t.Fatalf("prefetched file: %v, %d bytes", err, len(got))
	}
	var videoID string
	srv.DB.QueryRow(`SELECT video_id FROM ingest_jobs WHERE id = $1`, link.JobID).Scan(&videoID)
	if videoID != "vid1" {
		t.Errorf("video_id = %q, want vid1", videoID)
	}
	if job, err := ingest.ClaimNextJob(srv.DB); err != nil || job == nil || job.ID != link.JobID {
		t.Fatalf("ClaimNextJob after the hand-off = %+v, %v", job, err)
	}

	// Повторный запрос (перемотка после конца скачивания) — из того же файла.
	again := getAudio(t, ts.URL+link.URL, "bytes=0-")
	if b, _ := io.ReadAll(again.Body); again.StatusCode != http.StatusPartialContent || !bytes.Equal(b, file) {
		t.Errorf("request after download: %d, %d bytes", again.StatusCode, len(b))
	}
	if n := src.calls.Load(); n != 1 {
		t.Errorf("source opened %d times, want once", n)
	}
}

func TestDeezerStream_SignedLinkRequired(t *testing.T) {
	srv, ts, token, _ := newStreamTestServer(t)
	_, link := getStreamURL(t, ts, token, "12345")

	if res := getAudio(t, ts.URL+"/api/stream/deezer/12345", ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("no signature: %d, want 403", res.StatusCode)
	}
	other := strings.Replace(link.URL, "12345", "12346", 1)
	if res := getAudio(t, ts.URL+other, ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("signature of another track: %d, want 403", res.StatusCode)
	}
	expired := &media.Signer{Secret: srv.MediaSigner.Secret, TTL: -time.Minute}
	old, _ := expired.PathURL("/api/stream/deezer/12345")
	if res := getAudio(t, ts.URL+old, ""); res.StatusCode != http.StatusGone {
		t.Errorf("expired link: %d, want 410", res.StatusCode)
	}
	if code, _ := getStreamURL(t, ts, "", "12345"); code != http.StatusUnauthorized {
		t.Errorf("stream url without token: %d, want 401", code)
	}
}

func TestDeezerStream_LibraryTrackUsesMedia(t *testing.T) {
	srv, ts, token, src := newStreamTestServer(t)
	var artistID, albumID, trackID string
	srv.DB.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('A', '1') RETURNING id`).Scan(&artistID)
	srv.DB.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'B', '9') RETURNING id`, artistID).Scan(&albumID)
	srv.DB.QueryRow(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, gain_db, deezer_id)
	                 VALUES ($1, 'C', 1, 'abc.m4a', -3.5, '12345') RETURNING id`, albumID).Scan(&trackID)

	code, link := getStreamURL(t, ts, token, "12345")
	if code != http.StatusOK || link.TrackID != trackID || link.GainDB != -3.5 ||
		!strings.HasPrefix(link.URL, "/media/abc.m4a?st=") {
		t.Fatalf("stream url for a library track: %d %+v", code, link)
	}

	// Старая ссылка на поток, а трек тем временем попал в медиатеку.
	streamLink, _ := srv.MediaSigner.PathURL("/api/stream/deezer/12345")
	res := getAudio(t, ts.URL+streamLink, "")
	if res.StatusCode != http.StatusFound || !strings.HasPrefix(res.Header.Get("Location"), "/media/abc.m4a?st=") {
		t.Errorf("stream of a library track: %d → %q, want redirect to /media", res.StatusCode, res.Header.Get("Location"))
	}
	if n := src.calls.Load(); n != 0 {
		t.Errorf("source opened %d times for a library track", n)
	}
}

func TestDeezerStream_NotFoundOnYouTubeReleasesJob(t *testing.T) {
	srv, ts, token, src := newStreamTestServer(t)
	src.err = errors.New("match: no result with a matching title")
	_, link := getStreamURL(t, ts, token, "12345")

	res := getAudio(t, ts.URL+link.URL, "")
	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d, want 502", res.StatusCode)
	}
	// Job — воркеру как обычно (он попробует сам и при неудаче запишет ошибку).
	waitFor(t, "job release", func() bool {
		var released bool
		srv.DB.QueryRow(`SELECT hold_until IS NULL FROM ingest_jobs WHERE id = $1`, link.JobID).Scan(&released)
		return released
	})
	if _, err := os.Stat(ingest.PrefetchPath(srv.Streams.StagingDir, link.JobID)); !os.IsNotExist(err) {
		t.Errorf("prefetch file after a failed stream: %v", err)
	}
}

// Не нашлось на YouTube — несколько минут следующие запросы трека сразу
// получают 404 с причиной: плеер говорит, почему пропускает, а не ищет снова.
func TestDeezerStream_NotFoundIsRemembered(t *testing.T) {
	srv, ts, token, src := newStreamTestServer(t)
	src.err = fmt.Errorf("match: %w", ytdlp.ErrNoMatch)
	_, link := getStreamURL(t, ts, token, "12345")
	getAudio(t, ts.URL+link.URL, "").Body.Close()
	waitFor(t, "job release", func() bool {
		var released bool
		srv.DB.QueryRow(`SELECT hold_until IS NULL FROM ingest_jobs WHERE id = $1`, link.JobID).Scan(&released)
		return released
	})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream/deezer/12345/url", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Reason string }
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || body.Reason != "not_found" {
		t.Fatalf("second request: %d %+v", res.StatusCode, body)
	}
	// Админ указал видео — снова можно пробовать.
	srv.Streams.clearNotFound("12345")
	if srv.Streams.recentlyNotFound("12345") {
		t.Fatal("cleared")
	}
}

// Трек играют — его аудио не удалят как неслушаемое (не чаще раза в час).
func TestStreamURL_TouchesLastPlayed(t *testing.T) {
	db, router, token, srv := newTestServer(t)
	srv.MediaSigner = &media.Signer{Secret: "s3cret", TTL: time.Minute}
	var artistID, albumID, trackID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('A', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Al', '9') RETURNING id`, artistID).Scan(&albumID)
	db.QueryRow(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id, last_played_at)
	             VALUES ($1, $2, 'S', 1000, 'a.m4a', '12345', now() - interval '5 days') RETURNING id`, albumID, artistID).Scan(&trackID)
	call(t, router, "GET", "/api/tracks/"+trackID+"/stream-url", token, "", http.StatusOK)
	var fresh bool
	db.QueryRow(`SELECT last_played_at > now() - interval '1 minute' FROM tracks WHERE id = $1`, trackID).Scan(&fresh)
	if !fresh {
		t.Fatal("last_played_at not updated")
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		header     string
		start, end int64
		ok         bool
	}{
		{"bytes=0-", 0, 999, true},
		{"bytes=0-1", 0, 1, true},
		{"bytes=500-2000", 500, 999, true},
		{"bytes=-100", 900, 999, true},
		{"bytes=-5000", 0, 999, true},
		{"bytes=1000-", 0, 0, false},
		{"bytes=5-4", 0, 0, false},
		{"bytes=0-1,5-6", 0, 0, false},
		{"items=0-1", 0, 0, false},
		{"bytes=x-", 0, 0, false},
	}
	for _, c := range cases {
		start, end, ok := parseRange(c.header, 1000)
		if ok != c.ok || (ok && (start != c.start || end != c.end)) {
			t.Errorf("parseRange(%q) = %d, %d, %v; want %d, %d, %v", c.header, start, end, ok, c.start, c.end, c.ok)
		}
	}
}

// Та же запись на другом релизе (ISRC) уже в медиатеке: трек играет её файл
// сразу, без YouTube.
func TestDeezerStream_SameRecordingPlaysFromLibrary(t *testing.T) {
	srv, ts, token, src := newStreamTestServer(t)
	var artistID, albumID, twinID string
	srv.DB.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	srv.DB.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Single', '8') RETURNING id`, artistID).Scan(&albumID)
	srv.DB.QueryRow(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, gain_db, deezer_id, isrc)
	                 VALUES ($1, $2, 'Test Song', 200000, 'single.m4a', -1.5, '999', 'TEST00000001') RETURNING id`, albumID, artistID).Scan(&twinID)

	code, r := getStreamURL(t, ts, token, "12345") // тот же ISRC, другой релиз
	if code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if r.TrackID != twinID || !strings.HasPrefix(r.URL, "/media/single.m4a?") || r.GainDB != -1.5 {
		t.Fatalf("want the library file of the same recording, got %+v", r)
	}
	select {
	case w := <-src.want:
		t.Fatalf("YouTube was asked for %+v", w)
	case <-time.After(200 * time.Millisecond):
	}
	var isrc string
	srv.DB.QueryRow(`SELECT isrc FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).Scan(&isrc)
	if isrc != "TEST00000001" {
		t.Errorf("job isrc = %q", isrc)
	}
}
