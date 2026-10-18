package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"kaikku/internal/deezer"
)

// Поиск не ждёт фитов (запрос к Deezer на каждый трек): они догружаются
// фоном и видны со следующего поиска. Брошенный поиск их не заказывает, но
// выдачу запоминает: за неё уже заплачено.
func TestSearch_ContributorsInBackground(t *testing.T) {
	old := enrichLimit.Swap(deezer.NewLimiter(100, time.Second)) // не ждать общей доли
	t.Cleanup(func() { enrichLimit.Store(old) })
	var trackCalls, searchCalls atomic.Int32
	var leave atomic.Pointer[context.CancelFunc] // клиент уходит, пока идёт поиск
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		searchCalls.Add(1)
		if cancel := leave.Load(); cancel != nil {
			(*cancel)()
		}
		w.Write([]byte(`{"data":[{"id":41,"title":"Song","duration":100,"artist":{"id":1,"name":"A"},"album":{"id":2,"title":"B"}}],"total":1}`))
	})
	empty := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[],"total":0}`)) }
	mux.HandleFunc("/search/artist", empty)
	mux.HandleFunc("/search/album", empty)
	mux.HandleFunc("/track/41", func(w http.ResponseWriter, r *http.Request) {
		trackCalls.Add(1)
		w.Write([]byte(`{"id":41,"title":"Song","duration":100,"artist":{"id":1,"name":"A"},"album":{"id":2,"title":"B"},
			"contributors":[{"id":1,"name":"A","role":"Main"},{"id":3,"name":"C","role":"Main"}]}`))
	})
	dz := httptest.NewServer(mux)
	defer dz.Close()
	router := NewRouter(&Server{
		DB:        openTestDB(t),
		JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"),
		Deezer:    deezer.NewClientWithBaseURL(dz.URL),
	})
	token := registerTestUser(t, router)

	ctx, cancel := context.WithCancel(context.Background())
	leave.Store(&cancel)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=song", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(httptest.NewRecorder(), req)
	if n := trackCalls.Load(); n != 0 {
		t.Fatalf("abandoned search asked Deezer for %d tracks", n)
	}

	leave.Store(nil)
	if rec := request(router, http.MethodGet, "/api/search?q=song", token); rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	if n := searchCalls.Load(); n != 1 {
		t.Errorf("the abandoned search was not cached: Deezer searched %d times", n)
	}
	// Фиты — фоном (enrich.go), не дольше пары секунд.
	for i := 0; trackCalls.Load() == 0 && i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if trackCalls.Load() != 1 {
		t.Fatalf("featured artists not fetched in the background: %d track requests", trackCalls.Load())
	}

	// Тот же запрос ещё раз (с другим регистром) — из кеша, Deezer не
	// спрашивается, а фиты уже есть.
	before := searchCalls.Load()
	rec := request(router, http.MethodGet, "/api/search?q=Song", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("search again: %d", rec.Code)
	}
	if searchCalls.Load() != before || trackCalls.Load() != 1 {
		t.Fatalf("a repeated search went to Deezer: search %d→%d, tracks %d", before, searchCalls.Load(), trackCalls.Load())
	}
	var got struct {
		Tracks []deezer.Track `json:"tracks"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Tracks) != 1 || len(got.Tracks[0].Contributors) != 2 {
		t.Errorf("repeated search without the featured artists: %+v", got.Tracks)
	}
}
