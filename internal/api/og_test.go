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

// Обложка снятого с публикации релиза не попадает в превью публичного плейлиста.
func TestOG_PlaylistSkipsDraftCover(t *testing.T) {
	router, srv := ogFloodServer(t)
	rel := seedOwnRelease(t, srv.DB, "", false)
	var user, playlist string
	if err := srv.DB.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := srv.DB.QueryRow(`INSERT INTO playlists (user_id, title, is_public) VALUES ($1, 'Public', true) RETURNING id`, user).Scan(&playlist); err != nil {
		t.Fatal(err)
	}
	const cover = "/media/covers/custom/00112233445566778899aabbccddeeff.jpg"
	if _, err := srv.DB.Exec(`INSERT INTO playlist_items (playlist_id, position, deezer_id, meta) VALUES ($1, 1, $2, $3)`,
		playlist, rel.tracks[0], `{"cover_url":"`+cover+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, body := ogPage(t, router, "/playlist/"+playlist); !strings.Contains(body, cover) {
		t.Fatal("control: a published cover is missing from the preview")
	}
	if _, err := srv.DB.Exec(`UPDATE albums SET draft = true WHERE deezer_id = $1`, rel.album); err != nil {
		t.Fatal(err)
	}
	srv.ogCache = ttlCache{}
	if _, body := ogPage(t, router, "/playlist/"+playlist); strings.Contains(body, cover) {
		t.Error("a withdrawn release's cover is in the anonymous preview")
	}
}

// Ссылка «Поделиться» на трек (/album/<id>?track=<id>) — превью трека, а не
// всего альбома.
func TestOG_TrackLinkPreviewsTheTrack(t *testing.T) {
	db, _, admin, srv := newTestServer(t)
	srv.FrontendIndex = filepath.Join(t.TempDir(), "index.html")
	srv.PublicURL = "https://music.test"
	if err := os.WriteFile(srv.FrontendIndex, []byte(ogIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(srv)

	_, body := ogPage(t, router, "/album/9?track=12345")
	for _, want := range []string{
		`og:title" content="Test Song"`,
		`og:description" content="Song · Test Artist · Test Album"`,
		`og:type" content="music.song"`,
		`og:image" content="http://example.com/cover.jpg"`,
		`og:url" content="https://music.test/album/9?track=12345"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("track preview lacks %s: %s", want, body)
		}
	}
	if _, body := ogPage(t, router, "/album/9?track=888"); !strings.Contains(body, `content="Song · Test Artist, Feat Artist · Test Album"`) {
		t.Errorf("a song with a guest names every artist: %s", body)
	}

	// Трека на этом релизе нет или в ссылке мусор — превью альбома.
	album := func(path, why string) {
		t.Helper()
		if _, body := ogPage(t, router, path); !strings.Contains(body, `og:title" content="Test Album"`) ||
			!strings.Contains(body, `og:type" content="music.album"`) {
			t.Errorf("%s: %s", why, body)
		}
	}
	album("/album/9?track=501", "a song of another release previews the album")
	album("/album/9?track=404404", "an unknown song previews the album")
	album("/album/9?track=nope", "a malformed song id previews the album")
	if _, body := ogPage(t, router, "/album/9?track=nope"); !strings.Contains(body, `og:url" content="https://music.test/album/9"`) {
		t.Errorf("a malformed song id stays out of og:url: %s", body)
	}

	// Трек с другого релиза, подставленный в треклист этого, — тоже трек.
	if rec := postJSON(router, "/api/albums/deezer/9/claim", "", admin); rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := db.Exec(`UPDATE albums SET tracklist = '{12345,501}', tracklist_from = '{501}' WHERE deezer_id = '9'`); err != nil {
		t.Fatal(err)
	}
	srv.ogCache = ttlCache{}
	if _, body := ogPage(t, router, "/album/9?track=501"); !strings.Contains(body, `og:title" content="Album Track One"`) ||
		!strings.Contains(body, `content="Song · Test Artist · Test Album"`) {
		t.Errorf("a substituted song previews as a song of this release: %s", body)
	}
}

// Трек чужого черновика в превью не попадает.
func TestOG_TrackOfDraftStaysPrivate(t *testing.T) {
	router, srv := ogFloodServer(t)
	rel := seedOwnRelease(t, srv.DB, "", false)
	path := "/album/" + rel.album + "?track=" + rel.tracks[0]
	if _, body := ogPage(t, router, path); !strings.Contains(body, `og:title" content="First"`) {
		t.Fatalf("control: an uploaded song has a preview: %s", body)
	}
	if _, err := srv.DB.Exec(`UPDATE albums SET draft = true WHERE deezer_id = $1`, rel.album); err != nil {
		t.Fatal(err)
	}
	srv.ogCache = ttlCache{}
	if _, body := ogPage(t, router, path); strings.Contains(body, "First") || strings.Contains(body, "Own EP") {
		t.Errorf("a draft's song is in the anonymous preview: %s", body)
	}
}
