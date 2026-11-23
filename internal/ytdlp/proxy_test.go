package ytdlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func setProxyForTest(t *testing.T, raw string) {
	t.Helper()
	if err := SetProxy(raw); err != nil {
		t.Fatalf("SetProxy(%q): %v", raw, err)
	}
	t.Cleanup(func() { SetProxy("") })
}

func TestSetProxy_RejectsBadAddresses(t *testing.T) {
	for _, raw := range []string{"localhost:1080", "socks5://", "socks5://host", "ftp://host:21", "socks4://host:1080"} {
		if err := SetProxy(raw); err == nil {
			SetProxy("")
			t.Errorf("SetProxy(%q) accepted", raw)
		}
	}
}

func TestSetProxy_EveryYtDlpCallUsesIt(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("okA", "Song", 200))
	setProxyForTest(t, "socks5://127.0.0.1:1080")
	ctx := context.Background()

	if _, err := search(ctx, "ytsearch15:Artist Song"); err != nil {
		t.Fatalf("search: %v", err)
	}
	if _, err := Resolve(ctx, "okA"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := Download(ctx, "okA", t.TempDir(), "x"); err != nil {
		t.Fatalf("Download: %v", err)
	}
	body, err := Stream(ctx, "okA")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	io.ReadAll(body)
	body.Close()

	if len(*calls) != 4 {
		t.Fatalf("yt-dlp calls: %q, want 4", *calls)
	}
	for _, c := range *calls {
		if !strings.HasPrefix(c, "--proxy socks5://127.0.0.1:1080 ") {
			t.Errorf("yt-dlp %q runs without the proxy", c)
		}
	}
	if !slices.Contains(warmEnv(), "KAIKKU_YTDLP_PROXY=socks5://127.0.0.1:1080") {
		t.Errorf("helper env %q has no proxy", warmEnv())
	}
}

// Звук по ссылке googlevideo и поиск YouTube Music идут из Go, не из
// yt-dlp: им прокси нужен так же.
func TestSetProxy_GoRequestsGoThroughIt(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.String())
		mu.Unlock()
		if r.URL.Host == "media.test" {
			http.ServeContent(w, r, "", time.Time{}, strings.NewReader("audio"))
			return
		}
		http.Error(w, "teapot", http.StatusTeapot)
	}))
	defer proxySrv.Close()
	setProxyForTest(t, proxySrv.URL)

	body, err := Fetch(context.Background(), httpClient, &Media{URL: "http://media.test/v"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "audio" {
		t.Errorf("Fetch read %q", data)
	}

	old := musicSearchURL
	musicSearchURL = "http://music.test/youtubei/v1/search"
	t.Cleanup(func() { musicSearchURL = old })
	if _, err := searchMusic(context.Background(), "q"); err == nil || !strings.Contains(err.Error(), "418") {
		t.Errorf("searchMusic: %v, want the proxy's 418", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(seen, "GET http://media.test/v") || !slices.Contains(seen, "POST http://music.test/youtubei/v1/search") {
		t.Errorf("the proxy saw %q", seen)
	}
}
