package deezer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type countRT struct {
	n    atomic.Int32
	fail bool
}

func (rt *countRT) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.n.Add(1)
	if rt.fail {
		return nil, errors.New("proxyconnect tcp: connection refused")
	}
	return http.DefaultTransport.RoundTrip(r)
}

func trackServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return srv
}

const okTrack = `{"id":1,"title":"One","duration":100,"artist":{"id":7,"name":"A"}}`

func clientWithProxy(apiURL string, n int) (*Client, *countRT, *countRT) {
	direct, proxy := &countRT{}, &countRT{}
	c := NewClientWithBaseURL(apiURL).WithRateLimit(n, 5*time.Second)
	c.egresses[0].http = &http.Client{Transport: direct}
	c.egresses = append(c.egresses, &egress{name: "proxy", http: &http.Client{Transport: proxy}, limiter: newRateLimiter(n, 5*time.Second), proxy: true})
	return c, direct, proxy
}

func TestEgress_SplitsRequests(t *testing.T) {
	srv := trackServer(t, okTrack)
	c, direct, proxy := clientWithProxy(srv.URL, 1)
	start := time.Now()
	for _, id := range []string{"1", "2"} {
		if _, err := c.GetTrack(id); err != nil {
			t.Fatal(err)
		}
	}
	if direct.n.Load() != 1 || proxy.n.Load() != 1 {
		t.Errorf("direct %d, proxy %d; want 1 and 1", direct.n.Load(), proxy.n.Load())
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waited %s although the proxy was free", d)
	}
	if c.Capacity() != 2 {
		t.Errorf("capacity %d, want 2", c.Capacity())
	}
}

func TestEgress_ProxyDown(t *testing.T) {
	srv := trackServer(t, okTrack)
	direct, proxy := &countRT{}, &countRT{fail: true}
	c := NewClientWithBaseURL(srv.URL).WithRateLimit(10, 5*time.Second)
	c.egresses[0].http = &http.Client{Transport: direct}
	c.egresses = append([]*egress{{name: "proxy", proxy: true, limiter: newRateLimiter(10, 5*time.Second),
		http: &http.Client{Transport: proxy}}}, c.egresses...)
	if c.Capacity() != 20 {
		t.Errorf("capacity %d, want 20", c.Capacity())
	}
	if _, err := c.GetTrack("1"); err != nil {
		t.Fatalf("not retried directly: %v", err)
	}
	if proxy.n.Load() != 1 || direct.n.Load() != 1 {
		t.Errorf("direct %d, proxy %d; want 1 and 1", direct.n.Load(), proxy.n.Load())
	}
	if c.Capacity() != 10 {
		t.Errorf("capacity with the proxy down: %d, want 10", c.Capacity())
	}
	c.GetTrack("2")
	if proxy.n.Load() != 1 {
		t.Error("a downed proxy was tried again")
	}
}

func TestEgress_ProxyProbe(t *testing.T) {
	old := proxyDownFor
	proxyDownFor = 50 * time.Millisecond
	t.Cleanup(func() { proxyDownFor = old })
	srv := trackServer(t, okTrack)
	direct, proxy := &countRT{}, &countRT{fail: true}
	c := NewClientWithBaseURL(srv.URL)
	c.egresses[0].http = &http.Client{Transport: direct}
	p := &egress{name: "proxy", proxy: true, http: &http.Client{Transport: proxy}}
	c.egresses = append([]*egress{p}, c.egresses...)

	c.GetTrack("1")
	time.Sleep(60 * time.Millisecond)
	c.GetTrack("2")
	if proxy.n.Load() != 2 {
		t.Fatalf("proxy tried %d times, want 2 (first try and one probe)", proxy.n.Load())
	}
	time.Sleep(60 * time.Millisecond)
	c.GetTrack("3")
	if proxy.n.Load() != 2 {
		t.Error("the second pause did not double")
	}
	if p.up() {
		t.Error("a failing proxy counts as up")
	}
	proxy.fail = false
	time.Sleep(60 * time.Millisecond)
	c.GetTrack("4")
	if !p.up() {
		t.Fatal("a successful probe did not bring the proxy back")
	}
	c.GetTrack("5")
	if proxy.n.Load() != 4 {
		t.Errorf("proxy used %d times, want 4", proxy.n.Load())
	}
}

func TestEgress_OneProbe(t *testing.T) {
	p := &egress{name: "proxy", proxy: true, fails: 1}
	if !p.claim() {
		t.Fatal("the probe was not given")
	}
	if p.usable(false) || p.claim() {
		t.Error("a second request got through during the probe")
	}
	p.done(nil, proxyDownMax)
	if !p.up() || !p.claim() {
		t.Error("the proxy is not up after a good probe")
	}
}

func TestEgress_BurstIsOneFailure(t *testing.T) {
	p := &egress{name: "proxy", proxy: true}
	boom := errors.New("unexpected status 403")
	for range 4 {
		p.done(boom, proxyDownMax)
	}
	if p.fails != 1 {
		t.Errorf("%d failures from one burst, want 1", p.fails)
	}
	if left := time.Until(p.downUntil); left > proxyDownFor {
		t.Errorf("paused for %s, want at most %s", left, proxyDownFor)
	}
	p.downUntil = time.Now().Add(-time.Second)
	p.done(boom, proxyDownMax)
	if p.fails != 2 {
		t.Errorf("%d failures after the pause, want 2", p.fails)
	}
}

func TestRegion_OnlyWayKeepsPauseShort(t *testing.T) {
	old := proxyOnlyWayDownFor
	proxyOnlyWayDownFor = 30 * time.Millisecond
	t.Cleanup(func() { proxyOnlyWayDownFor = old })
	closed, _ := regionServer(t, false)
	c := NewClientWithBaseURL(closed.URL)
	c.checkRegions()
	p := &egress{name: "proxy", proxy: true, http: &http.Client{Transport: &countRT{fail: true}}}
	c.egresses = append(c.egresses, p)
	for range 3 {
		c.GetArtistAlbums("27", 10)
		if left := time.Until(p.downUntil); left > proxyOnlyWayDownFor {
			t.Fatalf("paused for %s, want at most %s", left, proxyOnlyWayDownFor)
		}
		time.Sleep(40 * time.Millisecond)
	}
	if p.fails != 3 {
		t.Errorf("%d failures, want 3", p.fails)
	}
}

func TestEgress_ProxyBadStatus(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(bad.Close)
	badURL, _ := url.Parse(bad.URL)
	c := NewClientWithBaseURL(trackServer(t, okTrack).URL)
	p := &egress{name: "proxy", proxy: true, http: &http.Client{Transport: rewriteRT{badURL}}}
	c.egresses = append([]*egress{p}, c.egresses...)
	if tr, err := c.GetTrack("1"); err != nil || tr.Title != "One" {
		t.Fatalf("not retried directly: %+v %v", tr, err)
	}
	if p.up() {
		t.Error("the proxy is still up after a bad answer")
	}
}

func TestEgress_ProxyOverQuota(t *testing.T) {
	quota := trackServer(t, `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`)
	ok := trackServer(t, okTrack)
	c := NewClientWithBaseURL(ok.URL)
	quotaURL, _ := url.Parse(quota.URL)
	c.egresses = append([]*egress{{name: "proxy", proxy: true, limiter: newRateLimiter(10, 5*time.Second),
		http: &http.Client{Transport: rewriteRT{quotaURL}}}}, c.egresses...)
	tr, err := c.GetTrack("1")
	if err != nil || tr.Title != "One" {
		t.Fatalf("not retried directly: %+v %v", tr, err)
	}
}

type rewriteRT struct{ to *url.URL }

func (rt rewriteRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = rt.to.Scheme, rt.to.Host
	return http.DefaultTransport.RoundTrip(r)
}

func regionServer(t *testing.T, open bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var albums atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/infos" && open:
			w.Write([]byte(`{"country_iso":"DE","open":true}`))
		case r.URL.Path == "/infos":
			w.Write([]byte(`{"country_iso":"HK","open":false}`))
		case open:
			albums.Add(1)
			w.Write([]byte(`{"data":[{"id":1,"title":"One"}],"total":1}`))
		default:
			albums.Add(1)
			w.Write([]byte(`{"data":[],"total":0}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &albums
}

func clientVia(t *testing.T, directURL, proxyURL string) (*Client, *egress) {
	t.Helper()
	to, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClientWithBaseURL(directURL)
	p := &egress{name: "proxy", proxy: true, http: &http.Client{Transport: rewriteRT{to}}}
	c.egresses = append(c.egresses, p)
	return c, p
}

func TestRegion_ClosedDirectGoesThroughProxy(t *testing.T) {
	closed, closedHits := regionServer(t, false)
	open, _ := regionServer(t, true)
	c, _ := clientVia(t, closed.URL, open.URL)
	c.checkRegions()
	albums, err := c.GetArtistAlbums("27", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 {
		t.Errorf("got %d albums through the proxy, want 1", len(albums))
	}
	if closedHits.Load() != 0 {
		t.Errorf("%d requests went through the closed way out", closedHits.Load())
	}
}

func TestRegion_AllClosedIsAnError(t *testing.T) {
	closed, closedHits := regionServer(t, false)
	c := NewClientWithBaseURL(closed.URL)
	c.checkRegions()
	if albums, err := c.GetArtistAlbums("27", 10); !errors.Is(err, errNoEgress) {
		t.Errorf("got %v, %v; want errNoEgress", albums, err)
	}
	if closedHits.Load() != 0 {
		t.Errorf("%d requests went through the closed way out", closedHits.Load())
	}
}

func TestRegion_ObjectsThroughClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/infos":
			w.Write([]byte(`{"country_iso":"HK","open":false}`))
		case "/album/5/tracks":
			w.Write([]byte(`{"data":[{"id":1,"title":"One"}],"total":1}`))
		default:
			w.Write([]byte(`{"id":5,"title":"One","name":"One"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClientWithBaseURL(srv.URL)
	c.checkRegions()
	album, err := c.GetAlbum("5")
	if err != nil || album.Title != "One" {
		t.Fatalf("album: %+v %v", album, err)
	}
	if tracks, err := c.GetAlbumTracks(album, 10); err != nil || len(tracks) != 1 {
		t.Errorf("tracklist: %+v %v", tracks, err)
	}
	if a, err := c.GetArtist("5"); err != nil || a.Name != "One" {
		t.Errorf("artist: %+v %v", a, err)
	}
	if tr, err := c.GetTrack("5"); err != nil || tr.Title != "One" {
		t.Errorf("track: %+v %v", tr, err)
	}
}

func TestRegion_ObjectsPreferOpen(t *testing.T) {
	closed, closedHits := regionServer(t, false)
	open := trackServer(t, okTrack)
	c, _ := clientVia(t, closed.URL, open.URL)
	c.checkRegion(c.egresses[0])
	if _, err := c.GetTrack("1"); err != nil {
		t.Fatal(err)
	}
	if closedHits.Load() != 0 {
		t.Errorf("%d requests went through the closed way out", closedHits.Load())
	}
}

func TestRegion_ProxyDownDirectClosed(t *testing.T) {
	closed, closedHits := regionServer(t, false)
	c := NewClientWithBaseURL(closed.URL)
	c.checkRegions()
	c.egresses = append(c.egresses, &egress{name: "proxy", proxy: true, http: &http.Client{Transport: &countRT{fail: true}}})
	_, err := c.GetArtistAlbums("27", 10)
	var transport *errTransport
	if !errors.As(err, &transport) {
		t.Errorf("got %v, want the proxy's error", err)
	}
	if closedHits.Load() != 0 {
		t.Errorf("%d requests went through the closed way out", closedHits.Load())
	}
	if _, err := c.GetArtistAlbums("27", 10); !errors.Is(err, errProxyPaused) {
		t.Errorf("got %v during the pause, want errProxyPaused", err)
	}
	if closedHits.Load() != 0 {
		t.Errorf("%d list requests went through the closed way out", closedHits.Load())
	}
	if _, err := c.GetTrack("1"); err != nil {
		t.Errorf("a track failed with the proxy paused: %v", err)
	}
	if closedHits.Load() != 1 {
		t.Errorf("%d requests through the closed way out, want the one track", closedHits.Load())
	}
}

func TestRegion_ProxyQuotaDirectClosed(t *testing.T) {
	closed, _ := regionServer(t, false)
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		w.Write([]byte(okTrack))
	}))
	t.Cleanup(proxy.Close)
	c, _ := clientVia(t, closed.URL, proxy.URL)
	c.checkRegion(c.egresses[0])
	if tr, err := c.GetTrack("1"); err != nil || tr.Title != "One" {
		t.Fatalf("not retried through the proxy: %+v %v", tr, err)
	}
}

func TestRegion_ReopensAndKeepsStateWithoutAnswer(t *testing.T) {
	var open atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/infos" {
			w.Write([]byte(okTrack))
			return
		}
		if open.Load() {
			w.Write([]byte(`{"country_iso":"DE","open":true}`))
		} else {
			w.Write([]byte(`{"country_iso":"HK","open":false}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClientWithBaseURL(srv.URL).WithRateLimit(10, 5*time.Second)
	d := c.egresses[0]

	c.checkRegions()
	if !d.isClosed() || c.Capacity() != 0 {
		t.Errorf("closed %v, capacity %d; want true, 0", d.isClosed(), c.Capacity())
	}
	working := d.http
	d.http = &http.Client{Transport: &countRT{fail: true}}
	c.checkRegions()
	if !d.isClosed() {
		t.Error("a failed check reopened the way out")
	}
	d.http = working

	open.Store(true)
	c.checkRegions()
	if d.isClosed() || c.Capacity() != 10 {
		t.Errorf("closed %v, capacity %d; want false, 10", d.isClosed(), c.Capacity())
	}
	if _, err := c.GetTrack("1"); err != nil {
		t.Errorf("not usable after reopening: %v", err)
	}
}

func TestWithProxy(t *testing.T) {
	u, _ := url.Parse("socks5://127.0.0.1:40000")
	c := NewClient().WithProxy(u, 40, 5*time.Second)
	if c.Capacity() != 80 || len(c.egresses) != 2 || !c.egresses[1].proxy {
		t.Errorf("capacity %d, egresses %d", c.Capacity(), len(c.egresses))
	}
}
