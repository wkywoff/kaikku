package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kaikku/internal/deezer"
)

const ogIndex = `<!doctype html><html><head><title>kaikku</title><!--og--><meta property="og:title" content="kaikku" /><!--/og--></head><body></body></html>`

func ogPage(t *testing.T, router http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil)) // без токена: бот превью не входит
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func TestOG_LinkPreviews(t *testing.T) {
	db, _, admin, srv := newTestServer(t)
	srv.FrontendIndex = filepath.Join(t.TempDir(), "index.html")
	srv.PublicURL = "https://music.test"
	os.WriteFile(srv.FrontendIndex, []byte(ogIndex), 0o644)
	router := NewRouter(srv)

	code, body := ogPage(t, router, "/album/9")
	if code != http.StatusOK || !strings.Contains(body, `property="og:title" content="Test Album"`) ||
		!strings.Contains(body, `og:description" content="Album · Test Artist`) ||
		!strings.Contains(body, `og:url" content="https://music.test/album/9"`) || strings.Count(body, `og:title`) != 1 {
		t.Fatalf("album: %d %s", code, body)
	}
	if _, body := ogPage(t, router, "/artist/1"); !strings.Contains(body, `og:title" content="Test Artist"`) {
		t.Fatalf("artist: %s", body)
	}
	// Неизвестное — страница как есть, без подробностей.
	if code, body := ogPage(t, router, "/album/not-an-id"); code != http.StatusOK || !strings.Contains(body, `og:title" content="kaikku"`) {
		t.Fatalf("unknown: %d %s", code, body)
	}

	// Плейлист: публичный — с названием (экранированным), приватный — нет.
	me := userIDOf(t, router, admin)
	var pub, priv string
	db.QueryRow(`INSERT INTO playlists (user_id, title, is_public) VALUES ($1, 'Rock "<b>"', true) RETURNING id`, me).Scan(&pub)
	db.QueryRow(`INSERT INTO playlists (user_id, title) VALUES ($1, 'Secret mix') RETURNING id`, me).Scan(&priv)
	if _, body := ogPage(t, router, "/playlist/"+pub); !strings.Contains(body, `content="Rock &#34;&lt;b&gt;&#34;"`) ||
		!strings.Contains(body, `content="Playlist · `) || !strings.Contains(body, ` · 0 songs"`) {
		t.Fatalf("public playlist: %s", body)
	}
	if _, body := ogPage(t, router, "/playlist/"+priv); strings.Contains(body, "Secret") {
		t.Fatalf("private playlist leaked: %s", body)
	}

	// Новая сборка фронта — новый index.html.
	os.WriteFile(srv.FrontendIndex, []byte(strings.Replace(ogIndex, "<body>", "<body>v2", 1)+" "), 0o644)
	if _, body := ogPage(t, router, "/album/9"); !strings.Contains(body, "v2") {
		t.Fatal("index.html is re-read after a deploy")
	}
}

func TestOG_OffWithoutIndex(t *testing.T) {
	_, router, _, _ := newTestServer(t)
	if code, _ := ogPage(t, router, "/album/9"); code != http.StatusNotFound {
		t.Fatalf("without FRONTEND_INDEX the API has no pages: %d", code)
	}
}

func ogFloodServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	_, _, _, srv := newTestServer(t)
	index := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(index, []byte(ogIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.FrontendIndex = index
	return NewRouter(srv), srv
}

// Перебор ID в превью не вытесняет общий кеш страниц.
func TestOG_FloodKeepsSharedCache(t *testing.T) {
	router, srv := ogFloodServer(t)
	loads := 0
	load := func() (string, error) { loads++; return "v", nil }
	cached(&srv.cache, "sentinel", time.Hour, load)
	for i := 0; i <= ttlCacheMaxEntries; i++ {
		if code, _ := ogPage(t, router, "/album/x"+strconv.Itoa(i)); code != http.StatusOK {
			t.Fatalf("preview: %d", code)
		}
	}
	cached(&srv.cache, "sentinel", time.Hour, load)
	if loads != 1 {
		t.Errorf("shared cache entry loaded %d times, want 1", loads)
	}
}

// Промахи превью ограничены, а страница без подробностей всё равно отдаётся.
func TestOG_MissesSpendLimitedDeezer(t *testing.T) {
	router, srv := ogFloodServer(t)
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	}))
	t.Cleanup(up.Close)
	srv.Deezer = deezer.NewClientWithBaseURL(up.URL)
	for i := range ogMissesPerMinute + 5 {
		if code, _ := ogPage(t, router, "/album/9000"+strconv.Itoa(i)); code != http.StatusOK {
			t.Fatalf("preview %d: %d", i, code)
		}
	}
	if n := int(hits.Load()); n == 0 || n > ogMissesPerMinute {
		t.Errorf("%d Deezer requests from %d previews, limit %d", n, ogMissesPerMinute+5, ogMissesPerMinute)
	}
}
