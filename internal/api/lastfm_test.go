package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"kaikku/internal/lastfm"
)

// fakeLastfm — Last.fm, который записывает запросы и отвечает reply(method).
type fakeLastfm struct {
	mu    sync.Mutex
	calls []url.Values
	reply func(method string) string
}

func (f *fakeLastfm) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, v := range f.calls {
		out = append(out, v.Get("method"))
	}
	return out
}

func (f *fakeLastfm) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func newLastfmTestServer(t *testing.T) (*sql.DB, http.Handler, string, *Server, *fakeLastfm) {
	t.Helper()
	db, router, token, srv := newTestServer(t)
	f := &fakeLastfm{reply: func(method string) string {
		if method == "auth.getSession" {
			return `{"session":{"name":"testuser","key":"sk1","subscriber":0}}`
		}
		return `{}`
	}}
	lf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		f.calls = append(f.calls, r.PostForm)
		reply := f.reply
		f.mu.Unlock()
		w.Write([]byte(reply(r.PostForm.Get("method"))))
	}))
	t.Cleanup(lf.Close)
	srv.Lastfm = &lastfm.Client{Key: "key", Secret: "secret", BaseURL: lf.URL}
	return db, router, token, srv, f
}

func lastfmStatus(t *testing.T, router http.Handler, token string) map[string]any {
	t.Helper()
	var st map[string]any
	json.Unmarshal(call(t, router, "GET", "/api/me/lastfm?cb=https://music.example/lastfm", token, "", http.StatusOK), &st)
	return st
}

func TestLastfm_OffWithoutKey(t *testing.T) {
	_, router, token, _ := newTestServer(t)
	if st := lastfmStatus(t, router, token); st["available"] != false {
		t.Fatalf("status = %v", st)
	}
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"t"}`, http.StatusNotFound)
	// Плеер шлёт всегда — без привязки это ничего не делает.
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, time.Now().Unix()), http.StatusNoContent)
}

func TestLastfm_LinkSettingsUnlink(t *testing.T) {
	db, router, token, _, f := newLastfmTestServer(t)

	st := lastfmStatus(t, router, token)
	if st["available"] != true || st["username"] != nil ||
		st["auth_url"] != "https://www.last.fm/api/auth/?api_key=key&cb=https%3A%2F%2Fmusic.example%2Flastfm" {
		t.Fatalf("before linking: %v", st)
	}
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"tok"}`, http.StatusOK)
	if f.last().Get("token") != "tok" {
		t.Fatalf("session request: %v", f.last())
	}
	st = lastfmStatus(t, router, token)
	if st["username"] != "testuser" || st["scrobble"] != true || st["love"] != true {
		t.Fatalf("linked: %v", st)
	}

	call(t, router, "PATCH", "/api/me/lastfm", token, `{"love":false}`, http.StatusOK)
	if st = lastfmStatus(t, router, token); st["scrobble"] != true || st["love"] != false {
		t.Fatalf("after patch: %v", st)
	}

	call(t, router, "DELETE", "/api/me/lastfm", token, "", http.StatusNoContent)
	var n int
	db.QueryRow(`SELECT count(*) FROM lastfm_links`).Scan(&n)
	if n != 0 {
		t.Fatal("link must be gone")
	}
	call(t, router, "PATCH", "/api/me/lastfm", token, `{"love":true}`, http.StatusNotFound)

	// Неверный токен — 400, а не «Last.fm недоступен».
	f.reply = func(string) string { return `{"error":4,"message":"Invalid authentication token"}` }
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"bad"}`, http.StatusBadRequest)
}

func TestLastfm_ScrobbleQueuedAndSent(t *testing.T) {
	db, router, token, srv, f := newLastfmTestServer(t)
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"tok"}`, http.StatusOK)

	started := time.Now().Add(-3 * time.Minute).Unix()
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, started), http.StatusNoContent)
	// Старше двух недель и из будущего Last.fm не примет.
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, time.Now().Add(-15*24*time.Hour).Unix()), http.StatusBadRequest)
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, time.Now().Add(time.Hour).Unix()), http.StatusBadRequest)

	srv.flushLastfm(context.Background())
	v := f.last()
	if v.Get("method") != "track.scrobble" || v.Get("sk") != "sk1" || v.Get("artist[0]") != "Test Artist" || v.Get("track[0]") != "Test Song" ||
		v.Get("album[0]") != "Test Album" || v.Get("duration[0]") != "200" || v.Get("timestamp[0]") != fmt.Sprint(started) {
		t.Fatalf("scrobble: %v", v)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM lastfm_queue`).Scan(&n)
	if n != 0 {
		t.Fatalf("queue = %d after a successful send", n)
	}

	// Скробблинг выключен — ничего не копится.
	call(t, router, "PATCH", "/api/me/lastfm", token, `{"scrobble":false}`, http.StatusOK)
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, started), http.StatusNoContent)
	db.QueryRow(`SELECT count(*) FROM lastfm_queue`).Scan(&n)
	if n != 0 {
		t.Fatal("scrobbling is off, nothing should be queued")
	}
}

func TestLastfm_RetryThenRevoked(t *testing.T) {
	db, router, token, srv, f := newLastfmTestServer(t)
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"tok"}`, http.StatusOK)
	call(t, router, "POST", "/api/me/scrobbles", token, fmt.Sprintf(`{"deezer_id":"12345","started_at":%d}`, time.Now().Unix()), http.StatusNoContent)

	f.reply = func(string) string { return `{"error":11,"message":"Service Offline"}` }
	srv.flushLastfm(context.Background())
	var attempts int
	var later bool
	if err := db.QueryRow(`SELECT attempts, next_try > now() FROM lastfm_queue`).Scan(&attempts, &later); err != nil || attempts != 1 || !later {
		t.Fatalf("after a failure: attempts=%d later=%v err=%v", attempts, later, err)
	}
	// Не пора — не отправляется.
	before := len(f.methods())
	srv.flushLastfm(context.Background())
	if len(f.methods()) != before {
		t.Fatal("a scrobble waiting for its retry must not be sent")
	}

	db.Exec(`UPDATE lastfm_queue SET next_try = now()`)
	f.reply = func(string) string { return `{"error":9,"message":"Invalid session key"}` }
	srv.flushLastfm(context.Background())
	var links, queued int
	db.QueryRow(`SELECT (SELECT count(*) FROM lastfm_links), (SELECT count(*) FROM lastfm_queue)`).Scan(&links, &queued)
	if links != 0 || queued != 0 {
		t.Fatalf("revoked session: links=%d queue=%d", links, queued)
	}
}

func TestLastfm_NowPlayingAndLove(t *testing.T) {
	_, router, token, _, f := newLastfmTestServer(t)
	call(t, router, "POST", "/api/me/lastfm", token, `{"token":"tok"}`, http.StatusOK)

	call(t, router, "POST", "/api/me/now-playing", token, `{"deezer_id":"12345"}`, http.StatusNoContent)
	if v := f.last(); v.Get("method") != "track.updateNowPlaying" || v.Get("track") != "Test Song" || v.Get("duration") != "200" {
		t.Fatalf("now playing: %v", v)
	}

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	if v := f.last(); v.Get("method") != "track.love" || v.Get("artist") != "Test Artist" || v.Get("track") != "Test Song" {
		t.Fatalf("love: %v", v)
	}
	call(t, router, "DELETE", "/api/me/liked/12345", token, "", http.StatusNoContent)
	if v := f.last(); v.Get("method") != "track.unlove" {
		t.Fatalf("unlove: %v", v)
	}
	// Снять несуществующий лайк — Last.fm не трогаем.
	before := len(f.methods())
	call(t, router, "DELETE", "/api/me/liked/12345", token, "", http.StatusNoContent)
	if len(f.methods()) != before {
		t.Fatal("no like, no unlove")
	}

	call(t, router, "PATCH", "/api/me/lastfm", token, `{"love":false}`, http.StatusOK)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	if strings.Contains(strings.Join(f.methods()[before:], ","), "track.love") {
		t.Fatal("love is off")
	}
}
