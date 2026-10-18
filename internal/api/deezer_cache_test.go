package api

import (
	"net/http"
	"testing"
	"time"

	"kaikku/internal/deezer"
)

// PGStore: запись, перезапись, срок.
func TestPGStore(t *testing.T) {
	db := openTestDB(t)
	db.Exec(`DELETE FROM deezer_cache WHERE key LIKE 'test:%'`)
	st := deezer.NewPGStore(db)
	st.Put("test:a", []byte(`{"v":1}`), time.Now().Add(time.Hour))
	st.Put("test:a", []byte(`{"v":2}`), time.Now().Add(time.Hour))
	if data, _, ok := st.Get("test:a"); !ok || string(data) != `{"v": 2}` {
		t.Errorf("get: %s %v", data, ok)
	}
	st.Put("test:b", []byte(`{}`), time.Now().Add(-time.Minute))
	if _, _, ok := st.Get("test:b"); ok {
		t.Error("an expired entry was returned")
	}
	if _, _, ok := st.Get("test:none"); ok {
		t.Error("a missing entry was returned")
	}
	got := st.GetMany([]string{"test:a", "test:b", "test:none"})
	if len(got) != 1 || string(got["test:a"].Data) != `{"v": 2}` {
		t.Errorf("get many: %v", got)
	}
}

// Страница артиста переживает перезапуск API: новый Server на той же базе
// не спрашивает Deezer.
func TestStoredCachedSurvivesRestart(t *testing.T) {
	db, _, _, srv := newTestServer(t)
	db.Exec(`DELETE FROM deezer_cache WHERE key = 'api:artist:1'`)
	srv.DeezerCache = deezer.NewPGStore(db)
	before := artistCalls.Load()
	if a, err := srv.artistInfo("1"); err != nil || a.Name != "Test Artist" {
		t.Fatalf("artist: %+v %v", a, err)
	}
	if artistCalls.Load() != before+1 {
		t.Fatalf("Deezer asked %d times, want once", artistCalls.Load()-before)
	}
	restarted := &Server{DB: db, Deezer: srv.Deezer, DeezerCache: deezer.NewPGStore(db)}
	if a, err := restarted.artistInfo("1"); err != nil || a.Name != "Test Artist" {
		t.Fatalf("artist after restart: %+v %v", a, err)
	}
	if artistCalls.Load() != before+1 {
		t.Error("the restarted API asked Deezer again")
	}
}

// «Обновить с Deezer»: альбом и треклист пропадают из кеша в базе.
func TestRefreshAlbum(t *testing.T) {
	db, router, token, srv := newTestServer(t)
	db.Exec(`DELETE FROM deezer_cache WHERE key LIKE 'album:9%' OR key LIKE 'tracklist:9:%'`)
	srv.Deezer.WithStore(deezer.NewPGStore(db))
	var page map[string]any
	getJSON(t, router, "/api/discover/albums/9", token, false, &page)
	count := func() (n int) {
		db.QueryRow(`SELECT count(*) FROM deezer_cache WHERE key = 'album:9' OR key LIKE 'tracklist:9:%'`).Scan(&n)
		return
	}
	if count() != 2 {
		t.Fatalf("cached rows: %d, want the album and its tracklist", count())
	}
	call(t, router, "POST", "/api/albums/deezer/9/refresh", token, ``, http.StatusNoContent)
	if count() != 0 {
		t.Errorf("rows left after refresh: %d", count())
	}
}

// Запись из базы живёт в памяти до своего срока в базе, а не ещё полный ttl.
func TestStoredCachedKeepsStoreExpiry(t *testing.T) {
	_, _, _, srv := newTestServer(t)
	srv.DeezerCache = soonStore{"api:artist:1": `{"id":"1","name":"Stored Artist"}`}
	if a, err := srv.artistInfo("1"); err != nil || a.Name != "Stored Artist" {
		t.Fatalf("artist from the store: %+v %v", a, err)
	}
	time.Sleep(60 * time.Millisecond)
	srv.DeezerCache = nil
	if a, err := srv.artistInfo("1"); err != nil || a.Name != "Test Artist" {
		t.Errorf("after the stored expiry: %+v %v, want a fresh one from Deezer", a, err)
	}
}

// soonStore — записи, которые устаревают через 50 мс.
type soonStore map[string]string

func (s soonStore) Get(key string) ([]byte, time.Time, bool) {
	v, ok := s[key]
	return []byte(v), time.Now().Add(50 * time.Millisecond), ok
}
func (s soonStore) Put(string, []byte, time.Time) {}
func (s soonStore) Forget(string)                 {}

// fixedStore — Store с заранее положенными записями.
type fixedStore map[string]string

func (s fixedStore) Get(key string) ([]byte, time.Time, bool) {
	v, ok := s[key]
	return []byte(v), time.Now().Add(time.Hour), ok
}
func (s fixedStore) Put(string, []byte, time.Time) {}
func (s fixedStore) Forget(string)                 {}

// Поиск берёт фиты из кеша, а не запросом на каждый трек.
func TestSearchContributorsFromCache(t *testing.T) {
	_, router, token, srv := newTestServer(t)
	srv.Deezer.WithStore(fixedStore{
		"track:12345": `{"id":"12345","title":"Test Song","artist":"Test Artist","artist_id":"1",
			"contributors":[{"id":"1","name":"Test Artist"},{"id":"3","name":"Feat Artist"}]}`,
	})
	var got struct {
		Tracks []deezer.Track `json:"tracks"`
	}
	getJSON(t, router, "/api/search?q=test+song", token, false, &got)
	for _, tr := range got.Tracks {
		if tr.ID == "12345" && len(tr.Contributors) != 2 {
			t.Errorf("cached contributors missing: %+v", tr)
		}
	}
}
