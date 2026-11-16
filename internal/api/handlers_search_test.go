package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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

// У небольших артистов Deezer ищет альбомы, а их треков в поиске треков нет:
// тогда треки берутся из найденных альбомов.
func TestSearch_TracksFromAlbums(t *testing.T) {
	var tracklists atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "big hit" {
			w.Write([]byte(`{"data":[{"id":7,"title":"Hit","duration":100,"artist":{"id":9,"name":"Big"},"album":{"id":8,"title":"Hits"}}],"total":1}`))
			return
		}
		// чужие треки с похожим именем, как Mortician на «mortrivial»
		w.Write([]byte(`{"data":[{"id":41,"title":"Rabid","duration":100,"artist":{"id":1,"name":"Mortician"},"album":{"id":2,"title":"B"}}],"total":1}`))
	})
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[],"total":0}`)) })
	mux.HandleFunc("/search/album", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"id":5,"title":"TIME DISTORTION","record_type":"ep","nb_tracks":2,"artist":{"id":3,"name":"mortrivial"}},
			{"id":6,"title":"Unrelated","record_type":"album","nb_tracks":1,"artist":{"id":4,"name":"Someone"}},
			{"id":8,"title":"Hits","record_type":"album","nb_tracks":1,"artist":{"id":9,"name":"Big"}}
		],"total":3}`))
	})
	mux.HandleFunc("/album/5/tracks", func(w http.ResponseWriter, r *http.Request) {
		tracklists.Add(1)
		w.Write([]byte(`{"data":[
			{"id":51,"title":"TIME DISTORTION","duration":120,"track_position":1,"disk_number":1,"artist":{"id":3,"name":"mortrivial"}},
			{"id":52,"title":"SAW","duration":130,"track_position":2,"disk_number":1,"artist":{"id":3,"name":"mortrivial"}}
		],"total":2}`))
	})
	mux.HandleFunc("/album/6/tracks", func(w http.ResponseWriter, r *http.Request) {
		t.Error("an album the query has nothing to do with was opened")
	})
	mux.HandleFunc("/album/8/tracks", func(w http.ResponseWriter, r *http.Request) { tracklists.Add(1) })
	dz := httptest.NewServer(mux)
	defer dz.Close()
	router := NewRouter(&Server{
		DB:        openTestDB(t),
		JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"),
		Deezer:    deezer.NewClientWithBaseURL(dz.URL),
	})
	token := registerTestUser(t, router)
	search := func(q string) []string {
		t.Helper()
		rec := request(router, http.MethodGet, "/api/search?q="+url.QueryEscape(q), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("search %q: %d %s", q, rec.Code, rec.Body.String())
		}
		var got struct{ Tracks []deezer.Track }
		json.Unmarshal(rec.Body.Bytes(), &got)
		var ids []string
		for _, tr := range got.Tracks {
			ids = append(ids, tr.ID+" "+tr.Title)
			if tr.ID == "51" && (tr.AlbumID != "5" || tr.Album != "TIME DISTORTION") {
				t.Errorf("a track from an album without its album: %+v", tr)
			}
		}
		return ids
	}

	// Название и исполнитель — только этот трек, другие с альбома нет; чужие — после.
	if got := search("mortrivial time distortion"); !slices.Equal(got, []string{"51 TIME DISTORTION", "41 Rabid"}) {
		t.Errorf("artist and title: %v", got)
	}
	// Одно имя — все его треки с найденных альбомов.
	if got := search("Mortrivial"); !slices.Equal(got, []string{"51 TIME DISTORTION", "52 SAW", "41 Rabid"}) {
		t.Errorf("artist only: %v", got)
	}
	// Поиск треков нашёл подходящий — альбомы не открываются.
	before := tracklists.Load()
	if got := search("big hit"); !slices.Equal(got, []string{"7 Hit"}) || tracklists.Load() != before {
		t.Errorf("a good track search still opened albums: %v, %d tracklists", got, tracklists.Load()-before)
	}
}

// Пустая выдача — пустые массивы, не null: страница поиска читает
// artists.length и с null зависала на «Ищем…».
func TestSearch_EmptyListsNotNull(t *testing.T) {
	empty := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[],"total":0}`)) }
	dz := httptest.NewServer(http.HandlerFunc(empty))
	defer dz.Close()
	router := NewRouter(&Server{
		DB:        openTestDB(t),
		JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"),
		Deezer:    deezer.NewClientWithBaseURL(dz.URL),
	})
	token := registerTestUser(t, router)

	rec := request(router, http.MethodGet, "/api/search?q=nothing", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &got)
	for _, k := range []string{"tracks", "artists", "albums", "users"} {
		if string(got[k]) != "[]" {
			t.Errorf("%s = %s, want []", k, got[k])
		}
	}
}
