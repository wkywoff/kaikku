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

// countRT считает запросы через выход; fail — отвечать ошибкой соединения.
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

// clientWithProxy — прямой выход и «прокси» (свой RoundTripper), по n
// запросов за 5 с каждый.
func clientWithProxy(apiURL string, n int) (*Client, *countRT, *countRT) {
	direct, proxy := &countRT{}, &countRT{}
	c := NewClientWithBaseURL(apiURL).WithRateLimit(n, 5*time.Second)
	c.egresses[0].http = &http.Client{Transport: direct}
	c.egresses = append(c.egresses, &egress{name: "proxy", http: &http.Client{Transport: proxy}, limiter: newRateLimiter(n, 5*time.Second), proxy: true})
	return c, direct, proxy
}

// Место кончилось на прямом — следующий запрос идёт через прокси, не ждёт.
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

// Прокси не отвечает — запрос уходит напрямую, прокси минуту не трогается.
func TestEgress_ProxyDown(t *testing.T) {
	srv := trackServer(t, okTrack)
	direct, proxy := &countRT{}, &countRT{fail: true}
	c := NewClientWithBaseURL(srv.URL).WithRateLimit(10, 5*time.Second)
	c.egresses[0].http = &http.Client{Transport: direct}
	// Прокси — первым, чтобы его попробовали.
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

// После паузы через прокси идёт одна проба; не прошла — пауза вдвое
// длиннее, прошла — прокси снова в деле.
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

	c.GetTrack("1") // сбой → пауза 50 мс
	time.Sleep(60 * time.Millisecond)
	c.GetTrack("2") // проба, снова сбой → пауза 100 мс
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
	c.GetTrack("4") // проба проходит
	if !p.up() {
		t.Fatal("a successful probe did not bring the proxy back")
	}
	c.GetTrack("5")
	if proxy.n.Load() != 4 {
		t.Errorf("proxy used %d times, want 4", proxy.n.Load())
	}
}

// Пока проба в пути, остальные запросы идут напрямую.
func TestEgress_OneProbe(t *testing.T) {
	p := &egress{name: "proxy", proxy: true, fails: 1}
	if !p.claim() {
		t.Fatal("the probe was not given")
	}
	if p.usable() || p.claim() {
		t.Error("a second request got through during the probe")
	}
	p.done(nil)
	if !p.up() || !p.claim() {
		t.Error("the proxy is not up after a good probe")
	}
}

// Не тот ответ через прокси (не 200) — повтор напрямую.
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

// «Квота» через прокси — повтор напрямую.
func TestEgress_ProxyOverQuota(t *testing.T) {
	quota := trackServer(t, `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`)
	ok := trackServer(t, okTrack)
	c := NewClientWithBaseURL(ok.URL)
	quotaURL, _ := url.Parse(quota.URL)
	// «Прокси» ведёт на сервер, который отвечает «квота».
	c.egresses = append([]*egress{{name: "proxy", proxy: true, limiter: newRateLimiter(10, 5*time.Second),
		http: &http.Client{Transport: rewriteRT{quotaURL}}}}, c.egresses...)
	tr, err := c.GetTrack("1")
	if err != nil || tr.Title != "One" {
		t.Fatalf("not retried directly: %+v %v", tr, err)
	}
}

// rewriteRT шлёт запрос на другой хост.
type rewriteRT struct{ to *url.URL }

func (rt rewriteRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = rt.to.Scheme, rt.to.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestWithProxy(t *testing.T) {
	u, _ := url.Parse("socks5://127.0.0.1:40000")
	c := NewClient().WithProxy(u, 40, 5*time.Second)
	if c.Capacity() != 80 || len(c.egresses) != 2 || !c.egresses[1].proxy {
		t.Errorf("capacity %d, egresses %d", c.Capacity(), len(c.egresses))
	}
}
