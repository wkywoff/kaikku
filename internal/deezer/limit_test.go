package deezer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter_HoldsTheWindow(t *testing.T) {
	l := newRateLimiter(3, 150*time.Millisecond)
	start := time.Now()
	for i := 0; i < 3; i++ {
		l.wait()
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("the first 3 requests should not wait, took %v", d)
	}
	l.wait() // четвёртый — только когда первый выйдет из окна
	if d := time.Since(start); d < 140*time.Millisecond {
		t.Errorf("the 4th request went through after %v, before the window passed", d)
	}
}

// Deezer отвечает на превышение лимита HTTP 200 с {"error":{"code":4}}.
func quotaServer(t *testing.T, failures int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failures {
			w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		w.Write([]byte(`{"id":27,"name":"Daft Punk"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestClient_RetriesWhenOverQuota(t *testing.T) {
	srv, calls := quotaServer(t, 2)
	a, err := NewClientWithBaseURL(srv.URL).GetArtist("27")
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if a.Name != "Daft Punk" || calls.Load() != 3 {
		t.Errorf("got %+v after %d calls, want Daft Punk after 3", a, calls.Load())
	}
}

func TestClient_GivesUpWhenQuotaPersists(t *testing.T) {
	srv, calls := quotaServer(t, 100)
	if _, err := NewClientWithBaseURL(srv.URL).GetArtist("27"); err == nil {
		t.Fatal("expected an error while Deezer keeps refusing")
	}
	if calls.Load() != 1+quotaRetries {
		t.Errorf("calls = %d, want %d", calls.Load(), 1+quotaRetries)
	}
}

func TestClient_GetTrackIsCachedAndCopied(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	trackCalls = 0
	first, err := c.GetTrack("3135553")
	if err != nil || len(first.Contributors) != 2 {
		t.Fatalf("GetTrack: %+v, %v", first, err)
	}
	// Вызывающий правит ответ на месте (правки админа) — кеш не должен это запомнить.
	first.Title = "changed"
	first.Contributors[0].Name = "changed"

	second, err := c.GetTrack("3135553")
	if err != nil {
		t.Fatal(err)
	}
	if trackCalls != 1 {
		t.Errorf("second GetTrack should come from the cache, calls = %d", trackCalls)
	}
	if second.Title == "changed" || second.Contributors[0].Name == "changed" {
		t.Errorf("the cache returned the caller's edits: %+v", second)
	}
}

func TestClient_GetTracksKeepsOrder(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	tracks, err := c.GetTracks([]string{"3135553", "3135553"})
	if err != nil || len(tracks) != 2 || tracks[0].ID != "3135553" || tracks[1].ID != "3135553" {
		t.Fatalf("GetTracks: %+v, %v", tracks, err)
	}
	if _, err := c.GetTracks([]string{"3135553", "404"}); err == nil {
		t.Error("an unknown track should fail the whole call")
	}
}

// Треклист альбома — из кеша; поля альбома каждый раз из переданного (он
// может быть с правками админа), а правка вызывающим не портит кеш.
func TestClient_AlbumTracksCached(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"data":[{"id":1,"title":"One","duration":100,"track_position":1,"disk_number":1,"artist":{"id":7,"name":"A"}}]}`))
	}))
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	first, err := c.GetAlbumTracks(&Album{ID: "5", Title: "Deezer title"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Title = "changed by the caller"
	second, err := c.GetAlbumTracks(&Album{ID: "5", Title: "Edited title"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("Deezer asked %d times, want once", calls.Load())
	}
	if second[0].Title != "One" || second[0].Album != "Edited title" || second[0].AlbumID != "5" {
		t.Errorf("cached track: %+v", second[0])
	}
}

// Короткое ожидание не считается; сводка обнуляет счёт.
func TestLimitStats(t *testing.T) {
	var s limitStats
	s.waited(10 * time.Millisecond)
	if s.count != 0 || s.scheduled {
		t.Fatal("a short wait was counted")
	}
	s.waited(2 * time.Second)
	s.waited(time.Second)
	s.overQuota()
	if s.count != 2 || s.longest != 2*time.Second || s.quota != 1 || !s.scheduled {
		t.Fatalf("stats: %+v", &s)
	}
	s.flush()
	if s.count != 0 || s.quota != 0 || s.scheduled {
		t.Errorf("after flush: %+v", &s)
	}
}

// Брошенный запрос уходит из очереди, не заняв места в окне.
func TestRateLimiter_WaitContextGivesUp(t *testing.T) {
	l := newRateLimiter(1, time.Second)
	l.wait()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := l.waitContext(ctx); err == nil {
		t.Fatal("waited past the context")
	}
	if len(l.times) != 1 {
		t.Errorf("the abandoned request took a slot: %d", len(l.times))
	}
}

func TestClient_AlbumRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/album/302127" {
			w.Write([]byte(`{"id":302127,"title":"Discovery","artist":{"id":27,"name":"Daft Punk"}}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":1,"title":"One","duration":100,"artist":{"id":27,"name":"Daft Punk"}}]}`))
	}))
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)
	if n := c.AlbumRequests("302127", 10); n != 2 {
		t.Errorf("uncached album: %d requests, want 2", n)
	}
	album, err := c.GetAlbum("302127")
	if err != nil {
		t.Fatal(err)
	}
	if n := c.AlbumRequests("302127", 10); n != 1 {
		t.Errorf("album cached: %d requests, want 1", n)
	}
	c.GetAlbumTracks(album, 10)
	if n := c.AlbumRequests("302127", 10); n != 0 {
		t.Errorf("both cached: %d requests, want 0", n)
	}
}
