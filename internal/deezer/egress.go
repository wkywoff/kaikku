package deezer

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const regionCheckEvery = 5 * time.Minute

var (
	errNoEgress    = errors.New("deezer: no open way out")
	errProxyPaused = errors.New("deezer: proxy is paused after a failure and direct is closed")
)

func (c *Client) unavailable() error {
	if c.openOther(nil) {
		return errProxyPaused
	}
	return errNoEgress
}

type egress struct {
	name    string
	http    *http.Client
	limiter *rateLimiter
	proxy   bool

	mu        sync.Mutex
	fails     int
	downUntil time.Time
	probing   bool
	closed    bool
}

var (
	proxyDownFor        = time.Minute
	proxyOnlyWayDownFor = 10 * time.Second
)

const proxyDownMax = 10 * time.Minute

func (e *egress) up() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fails == 0 && !e.closed
}

func (e *egress) usable(closedToo bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed && !closedToo {
		return false
	}
	return e.fails == 0 || !e.probing && !time.Now().Before(e.downUntil)
}

func (e *egress) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func (e *egress) setClosed(closed bool, country string) {
	e.mu.Lock()
	was := e.closed
	e.closed = closed
	e.mu.Unlock()
	switch {
	case closed && !was:
		log.Printf("deezer: %s is closed, Deezer sees it in %q and withholds lists from it", e.name, country)
	case !closed && was:
		log.Printf("deezer: %s is open again (%q)", e.name, country)
	}
}

func (c *Client) WatchRegions(ctx context.Context) {
	c.checkRegions()
	go func() {
		t := time.NewTicker(regionCheckEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.checkRegions()
			}
		}
	}()
}

func (c *Client) checkRegions() {
	wasOpen := c.openOther(nil)
	defer func() {
		if wasOpen && !c.openOther(nil) {
			log.Printf("deezer: no open way out: discographies, top tracks and search will fail, single albums and tracks still work; set DEEZER_PROXY to a proxy in another country")
		}
	}()
	var wg sync.WaitGroup
	for _, e := range c.egresses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.checkRegion(e)
		}()
	}
	wg.Wait()
}

func (c *Client) checkRegion(e *egress) {
	var info struct {
		Country string `json:"country_iso"`
		Open    *bool  `json:"open"`
	}
	if err := c.getOnce(e, "/infos", &info); err != nil || info.Open == nil {
		return
	}
	e.setClosed(!*info.Open, info.Country)
}

func (c *Client) openOther(e *egress) bool {
	for _, o := range c.egresses {
		if o != e && !o.isClosed() {
			return true
		}
	}
	return false
}

func (e *egress) claim() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fails == 0 {
		return true
	}
	if e.probing || time.Now().Before(e.downUntil) {
		return false
	}
	e.probing = true
	return true
}

func (e *egress) done(err error, maxPause time.Duration) {
	var apiErr *APIError
	failed := err != nil && !errors.As(err, &apiErr)
	e.mu.Lock()
	e.probing = false
	was := e.fails
	if !failed {
		e.fails = 0
		e.mu.Unlock()
		if was > 0 {
			log.Printf("deezer: %s is back", e.name)
		}
		return
	}
	if was > 0 && time.Now().Before(e.downUntil) {
		e.mu.Unlock()
		return
	}
	e.fails++
	pause := proxyDownFor << min(e.fails-1, 10)
	pause = min(pause, maxPause)
	e.downUntil = time.Now().Add(pause)
	e.mu.Unlock()
	log.Printf("deezer: %s failed, not using it for %s: %v", e.name, pause, err)
}

func (c *Client) pauseLimit(e *egress) time.Duration {
	if c.openOther(e) {
		return proxyDownMax
	}
	return proxyOnlyWayDownFor
}

func (c *Client) WithProxy(proxy *url.URL, n int, per time.Duration) *Client {
	c.egresses = append(c.egresses, &egress{
		name: "proxy " + proxy.Host,
		http: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy), TLSHandshakeTimeout: 3 * time.Second,
		}},
		limiter: newRateLimiter(n, per),
		proxy:   true,
	})
	return c
}

func (c *Client) Capacity() int {
	total := 0
	for _, e := range c.egresses {
		if e.limiter == nil {
			return 0
		}
		if e.up() {
			total += int(int64(e.limiter.n) * int64(5*time.Second) / int64(e.limiter.per))
		}
	}
	return total
}

func (c *Client) pick(avoid *egress, closedToo bool) (*egress, time.Duration) {
	start := time.Now()
	for {
		var next *egress
		var nextIn time.Duration
		for _, e := range c.egresses {
			if e == avoid || !e.usable(closedToo) {
				continue
			}
			if e.limiter == nil {
				if e.claim() {
					return e, time.Since(start)
				}
				continue
			}
			ok, in := e.limiter.tryTake()
			if ok && e.claim() {
				return e, time.Since(start)
			}
			if ok {
				continue
			}
			if next == nil || in < nextIn {
				next, nextIn = e, in
			}
		}
		if next == nil {
			return nil, time.Since(start)
		}
		time.Sleep(nextIn)
	}
}

type errTransport struct{ err error }

func (e *errTransport) Error() string { return "request: " + e.err.Error() }
func (e *errTransport) Unwrap() error { return e.err }
