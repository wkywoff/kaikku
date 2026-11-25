package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	loginFreeAttempts     = 5
	loginNameFreeAttempts = 50
	loginPauseFirst       = 30 * time.Second
	loginPauseMax         = 15 * time.Minute
	loginWindow           = 24 * time.Hour
	loginMaxTracked       = 10000
)

type loginGuard struct {
	mu sync.Mutex
	m  map[string]*loginFailures
}

type loginFailures struct {
	count int
	last  time.Time
}

func loginPause(count, free int) time.Duration {
	if count < free {
		return 0
	}
	d := loginPauseFirst
	for i := free; i < count && d < loginPauseMax; i++ {
		d *= 2
	}
	return min(d, loginPauseMax)
}

func (g *loginGuard) wait(name string, now time.Time) time.Duration {
	return g.waitFree(name, now, loginFreeAttempts)
}

func (g *loginGuard) waitFree(name string, now time.Time, free int) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.m[name]
	if f == nil || now.Sub(f.last) > loginWindow {
		return 0
	}
	return max(0, loginPause(f.count, free)-now.Sub(f.last))
}

func (g *loginGuard) failed(name string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil || len(g.m) >= loginMaxTracked {
		g.prune(now)
	}
	f := g.m[name]
	if f == nil || now.Sub(f.last) > loginWindow {
		f = &loginFailures{}
		g.m[name] = f
	}
	f.count++
	f.last = now
}

func (g *loginGuard) reserve(name string, now time.Time, free int) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.m[name]; f != nil && now.Sub(f.last) <= loginWindow {
		if wait := loginPause(f.count, free) - now.Sub(f.last); wait > 0 {
			return wait
		}
	}
	if g.m == nil || len(g.m) >= loginMaxTracked {
		g.prune(now)
	}
	f := g.m[name]
	if f == nil || now.Sub(f.last) > loginWindow {
		f = &loginFailures{}
		g.m[name] = f
	}
	f.count++
	f.last = now
	return 0
}

func (g *loginGuard) refund(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.m[name]; f != nil {
		if f.count--; f.count <= 0 {
			delete(g.m, name)
		}
	}
}

func (g *loginGuard) succeeded(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, name)
}

func (g *loginGuard) prune(now time.Time) {
	if g.m == nil {
		g.m = map[string]*loginFailures{}
		return
	}
	for name, f := range g.m {
		if now.Sub(f.last) > loginWindow {
			delete(g.m, name)
		}
	}
	if len(g.m) >= loginMaxTracked {
		for name, f := range g.m {
			if f.count < loginFreeAttempts {
				delete(g.m, name)
			}
		}
	}
	if len(g.m) >= loginMaxTracked {
		g.m = map[string]*loginFailures{}
	}
}

func (g *loginGuard) count(name string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.m[name]; f != nil {
		return f.count
	}
	return 0
}

func (g *loginGuard) forgetPrefix(prefix string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for name := range g.m {
		if strings.HasPrefix(name, prefix) {
			delete(g.m, name)
		}
	}
}

func clientIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		ip = r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is6() {
		if subnet, err := addr.Prefix(64); err == nil {
			return subnet.String()
		}
	}
	return addr.String()
}

func loginPairKey(name, ip string) string { return name + "\x00" + ip }

func (s *Server) reserveLogin(name, ip string, now time.Time) time.Duration {
	pair := loginPairKey(name, ip)
	if wait := s.logins.reserve(pair, now, loginFreeAttempts); wait > 0 {
		return wait
	}
	var wait time.Duration
	if s.logins.count(pair) <= loginFreeAttempts {
		wait = s.loginNames.reserve(name, now, loginNameFreeAttempts)
	} else {
		wait = s.loginNames.waitFree(name, now, loginNameFreeAttempts)
	}
	if wait > 0 {
		s.logins.refund(pair)
	}
	return wait
}

func (s *Server) refundLogin(name, ip string) {
	pair := loginPairKey(name, ip)
	first := s.logins.count(pair) <= loginFreeAttempts
	s.logins.refund(pair)
	if first {
		s.loginNames.refund(name)
	}
}

func (s *Server) loginSucceeded(name, ip string) {
	s.logins.succeeded(loginPairKey(name, ip))
	s.loginNames.succeeded(name)
}

func (s *Server) loginForget(name string) {
	s.logins.forgetPrefix(name + "\x00")
	s.loginNames.succeeded(name)
}
