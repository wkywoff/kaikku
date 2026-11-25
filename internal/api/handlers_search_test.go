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

func TestSearch_ContributorsInBackground(t *testing.T) {
	old := enrichLimit.Swap(deezer.NewLimiter(100, time.Second))
	t.Cleanup(func() { enrichLimit.Store(old) })
	var trackCalls, searchCalls atomic.Int32
	var leave atomic.Pointer[context.CancelFunc]
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
	for i := 0; trackCalls.Load() == 0 && i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if trackCalls.Load() != 1 {
		t.Fatalf("featured artists not fetched in the background: %d track requests", trackCalls.Load())
	}

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

func TestSearch_TracksFromAlbums(t *testing.T) {
	var tracklists atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "big hit" {
			w.Write([]byte(`{"data":[{"id":7,"title":"Hit","duration":100,"artist":{"id":9,"name":"Big"},"album":{"id":8,"title":"Hits"}}],"total":1}`))
			return
		}
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

	if got := search("mortrivial time distortion"); !slices.Equal(got, []string{"51 TIME DISTORTION", "41 Rabid"}) {
		t.Errorf("artist and title: %v", got)
	}
	if got := search("Mortrivial"); !slices.Equal(got, []string{"51 TIME DISTORTION", "52 SAW", "41 Rabid"}) {
		t.Errorf("artist only: %v", got)
	}
	before := tracklists.Load()
	if got := search("big hit"); !slices.Equal(got, []string{"7 Hit"}) || tracklists.Load() != before {
		t.Errorf("a good track search still opened albums: %v, %d tracklists", got, tracklists.Load()-before)
	}
}

func TestSearch_TracksFromFreshReleases(t *testing.T) {
	var releaseLists, byName atomic.Int32
	today := time.Now().Format("2006-01-02")
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":41,"title":"Казино","duration":100,"artist":{"id":3,"name":"5opka"},"album":{"id":2,"title":"Казино"}}],"total":1}`))
	})
	mux.HandleFunc("/search/album", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[],"total":0}`)) })
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "5opka":
			byName.Add(1)
			w.Write([]byte(`{"data":[{"id":4,"name":"5opka band"},{"id":3,"name":"5opka","nb_fan":5}],"total":2}`))
		default:
			w.Write([]byte(`{"data":[],"total":0}`))
		}
	})
	mux.HandleFunc("/artist/3/albums", func(w http.ResponseWriter, r *http.Request) {
		releaseLists.Add(1)
		w.Write([]byte(`{"data":[
			{"id":20,"title":"Старый Русский","release_date":"2020-01-01","record_type":"single"},
			{"id":21,"title":"Русский Канье","release_date":"` + today + `","record_type":"single"}
		],"total":2}`))
	})
	mux.HandleFunc("/album/20/tracks", func(w http.ResponseWriter, r *http.Request) {
		t.Error("an old release was opened: it is in Deezer's search already")
	})
	mux.HandleFunc("/album/21/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":51,"title":"Русский Канье","duration":150,"track_position":1,"disk_number":1,"artist":{"id":3,"name":"5opka"}}],"total":1}`))
	})
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
			if tr.ID == "51" && (tr.AlbumID != "21" || tr.Album != "Русский Канье") {
				t.Errorf("a fresh track without its release: %+v", tr)
			}
		}
		return ids
	}

	if got := search("5opka"); !slices.Equal(got, []string{"41 Казино"}) || releaseLists.Load() != 0 {
		t.Errorf("artist only: %v, %d release lists", got, releaseLists.Load())
	}
	for _, q := range []string{"5opka р", "5opka русс", "5opka русский", "5opka Русский Канье"} {
		if got := search(q); !slices.Equal(got, []string{"51 Русский Канье", "41 Казино"}) {
			t.Errorf("%q: %v", q, got)
		}
	}
	if releaseLists.Load() != 1 || byName.Load() != 1 {
		t.Errorf("Deezer asked %d times for the release list and %d for the artist", releaseLists.Load(), byName.Load())
	}
	if got := search("5OPKA канье"); !slices.Equal(got, []string{"51 Русский Канье", "41 Казино"}) {
		t.Errorf("typed at once: %v", got)
	}
	if got := search("5opka завоз"); !slices.Equal(got, []string{"41 Казино"}) {
		t.Errorf("no such release: %v", got)
	}
	for _, q := range []string{"русский канье", "русский канье 5opka"} {
		if got := search(q); slices.Contains(got, "51 Русский Канье") {
			t.Errorf("%q: %v", q, got)
		}
	}
}

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
