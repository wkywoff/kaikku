package deezer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memStore — Store в памяти для тестов.
type memStore struct {
	mu sync.Mutex
	m  map[string]memEntry
}

type memEntry struct {
	data    []byte
	expires time.Time
}

func (s *memStore) Get(key string) ([]byte, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || time.Now().After(e.expires) {
		return nil, time.Time{}, false
	}
	return e.data, e.expires, true
}

func (s *memStore) Put(key string, data []byte, expires time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]memEntry{}
	}
	s.m[key] = memEntry{data, expires}
}

func (s *memStore) Forget(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.m {
		if k == key || strings.HasPrefix(k, key+":") {
			delete(s.m, k)
		}
	}
}

// Второй клиент (перезапуск) на том же store не спрашивает Deezer.
func TestClient_StoreSurvivesRestart(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch {
		case r.URL.Path == "/track/5":
			w.Write([]byte(`{"id":5,"title":"Five","duration":100,"artist":{"id":1,"name":"A"},
				"contributors":[{"id":1,"name":"A","role":"Main"},{"id":2,"name":"B","role":"Main"}]}`))
		case r.URL.Path == "/album/9":
			w.Write([]byte(`{"id":9,"title":"Nine","artist":{"id":1,"name":"A"}}`))
		case strings.HasPrefix(r.URL.Path, "/album/9/tracks"):
			w.Write([]byte(`{"data":[{"id":5,"title":"Five","duration":100,"artist":{"id":1,"name":"A"}}]}`))
		}
	}))
	defer srv.Close()
	st := &memStore{}

	first := NewClientWithBaseURL(srv.URL).WithStore(st)
	first.GetTrack("5")
	album, _ := first.GetAlbum("9")
	first.GetAlbumTracks(album, 50)
	if calls.Load() != 3 {
		t.Fatalf("first client: %d calls, want 3", calls.Load())
	}
	if len(st.m) != 3 || st.m["track:5"].data == nil || st.m["album:9"].data == nil || st.m["tracklist:9:50"].data == nil {
		t.Fatalf("store keys: %v", st.m)
	}

	second := NewClientWithBaseURL(srv.URL).WithStore(st)
	tr, err := second.GetTrack("5")
	if err != nil || len(tr.Contributors) != 2 {
		t.Errorf("track from store: %+v %v", tr, err)
	}
	a, _ := second.GetAlbum("9")
	tracks, _ := second.GetAlbumTracks(a, 50)
	if !second.HasTrack("5") || second.AlbumRequests("9", 50) != 0 || len(tracks) != 1 || tracks[0].Album != "Nine" {
		t.Errorf("album from store: %+v %+v", a, tracks)
	}
	if calls.Load() != 3 {
		t.Errorf("restarted client asked Deezer: %d calls", calls.Load())
	}

	// Устаревшее в store не берётся.
	st.m["track:5"] = memEntry{st.m["track:5"].data, time.Now().Add(-time.Minute)}
	if NewClientWithBaseURL(srv.URL).WithStore(st).HasTrack("5") {
		t.Error("an expired track taken from the store")
	}
}

// Свежий релиз и неполный треклист держатся в кеше часы, а не неделю.
func TestClient_FreshReleaseShortTTL(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/album/1":
			w.Write([]byte(`{"id":1,"title":"New","release_date":"` + today + `","artist":{"id":1,"name":"A"}}`))
		case r.URL.Path == "/album/2":
			w.Write([]byte(`{"id":2,"title":"Old","release_date":"2001-03-07","artist":{"id":1,"name":"A"}}`))
		case strings.HasSuffix(r.URL.Path, "/tracks"):
			w.Write([]byte(`{"data":[{"id":5,"title":"Five","duration":100,"artist":{"id":1,"name":"A"}}]}`))
		}
	}))
	defer srv.Close()
	st := &memStore{}
	c := NewClientWithBaseURL(srv.URL).WithStore(st)
	short := func(key string) bool { return time.Until(st.m[key].expires) <= freshCacheTTL }

	fresh, _ := c.GetAlbum("1")
	c.GetAlbumTracks(fresh, 50)
	if !short("album:1") || !short("tracklist:1:50") {
		t.Error("a fresh release is kept for long")
	}
	old, _ := c.GetAlbum("2")
	old.NbTracks = 1
	c.GetAlbumTracks(old, 50)
	if short("album:2") || short("tracklist:2:50") {
		t.Error("an old complete release is kept for hours only")
	}
	old.NbTracks = 12 // в треклисте 1 трек из 12
	c.GetAlbumTracks(old, 100)
	if !short("tracklist:2:100") {
		t.Error("an incomplete tracklist is kept for long")
	}
}

// batchMem — memStore с GetMany; считает обращения.
type batchMem struct {
	memStore
	gets, many atomic.Int32
}

func (s *batchMem) Get(key string) ([]byte, time.Time, bool) {
	s.gets.Add(1)
	return s.memStore.Get(key)
}

func (s *batchMem) GetMany(keys []string) map[string]StoreEntry {
	s.many.Add(1)
	out := map[string]StoreEntry{}
	for _, k := range keys {
		if data, exp, ok := s.memStore.Get(k); ok {
			out[k] = StoreEntry{data, exp}
		}
	}
	return out
}

// Фиты из кеша для многих треков — одним запросом к store; трек, которого
// нет нигде, возвращается для фоновой догрузки.
func TestEnrichContributorsCached_Batch(t *testing.T) {
	st := &batchMem{}
	exp := time.Now().Add(time.Hour)
	st.Put("track:1", []byte(`{"id":"1","contributors":[{"id":"7","name":"A"},{"id":"8","name":"B"}]}`), exp)
	st.Put("track:2", []byte(`{"id":"2","contributors":[{"id":"7","name":"A"}]}`), exp)
	c := NewClientWithBaseURL("http://unused").WithStore(st)
	c.GetTrack("2") // в памяти: в store за ним не ходят
	st.gets.Store(0)
	tracks := []Track{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4", Contributors: []ArtistRef{{ID: "9"}}}}
	missing := c.EnrichContributorsCached(tracks)
	if len(tracks[0].Contributors) != 2 || len(tracks[1].Contributors) != 1 {
		t.Errorf("contributors: %+v", tracks)
	}
	if len(missing) != 1 || missing[0] != "3" {
		t.Errorf("missing %v, want [3]", missing)
	}
	if st.many.Load() != 1 || st.gets.Load() != 0 {
		t.Errorf("store asked %d times in a batch and %d one by one, want 1 and 0", st.many.Load(), st.gets.Load())
	}
	c.EnrichContributorsCached([]Track{{ID: "1"}})
	if st.many.Load() != 1 {
		t.Error("a track read from the store was not kept in memory")
	}
}

// Одновременные промахи по одному треку — один запрос к Deezer, и каждый
// получает свою копию.
func TestClient_ConcurrentMissesShareRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`{"id":5,"title":"Five","duration":100,"artist":{"id":1,"name":"A"},
			"contributors":[{"id":1,"name":"A","role":"Main"}]}`))
	}))
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)
	var wg sync.WaitGroup
	got := make([]*Track, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = c.GetTrack("5")
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1", calls.Load())
	}
	got[0].Contributors[0].Name = "changed"
	if got[1].Contributors[0].Name != "A" {
		t.Error("callers share one contributors slice")
	}
}
