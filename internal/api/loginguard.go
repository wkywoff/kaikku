package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Защита от подбора пароля. Настоящего адреса клиента API может не видеть
// (за прокси все запросы приходят с одного адреса, и лимит nginx на
// /api/login тогда общий на всех).
// Поэтому неудачи считаются по паре имя + адрес (X-Real-IP от nginx): после
// loginFreeAttempts неудач подряд пауза перед следующей попыткой растёт —
// 30 с, 1, 2, 4, 8 минут, дальше раз в 15 минут: около сотни попыток в
// сутки с адреса (IPv6 — с подсети /64, clientIP). Один чужой адрес хозяина
// имени не задевает.
// Перебор с многих адресов упирается в потолок на имя (loginNameFreeAttempts).
// Пока идёт его пауза, под этим именем не входит никто, и хозяин тоже: это
// цена защиты от распределённого перебора (открытые сессии работают дальше).
// В потолок идут только первые попытки с каждого адреса, чтобы один адрес его
// не выбрал; поэтому уже начавший адрес дальше ограничен только своей паузой,
// и общий темп перебора растёт с числом адресов.
// Ссылка сброса пароля (handlers_password.go) снимает всё сразу.

const (
	loginFreeAttempts     = 5
	loginNameFreeAttempts = 50
	loginPauseFirst       = 30 * time.Second
	loginPauseMax         = 15 * time.Minute
	loginWindow           = 24 * time.Hour // неудачи старше этого забываются
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

// loginPause — сколько ждать после count неудач подряд, из них free без паузы.
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

// wait reports how long name has to wait before the next attempt (0 — go).
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

// reserve is wait and failed in one step, for checks that take long: иначе
// параллельные запросы успели бы все пройти wait до первой неудачи. Попытка
// считается сразу; verdict — succeeded (снимает счёт) или refund (не было проверки).
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

// refund returns an attempt that reserve counted but no password was checked.
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

// prune drops forgotten failures, then names that have no pause yet (a flood
// of made-up names must not lift a pause someone has earned); if the map is
// still full, it starts over.
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

// count — сколько попыток уже на счету.
func (g *loginGuard) count(name string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.m[name]; f != nil {
		return f.count
	}
	return 0
}

// forgetPrefix снимает счёт со всех ключей с этим началом.
func (g *loginGuard) forgetPrefix(prefix string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for name := range g.m {
		if strings.HasPrefix(name, prefix) {
			delete(g.m, name)
		}
	}
}

// clientIP — адрес клиента для счёта попыток: nginx кладёт его в X-Real-IP и
// затирает присланный извне; порт API наружу не открыт. Без nginx (разработка)
// — адрес соединения. IPv6 — подсеть /64: у одного абонента их целая подсеть,
// и каждый её адрес иначе считался бы новым.
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

// reserveLogin считает попытку входа с этого адреса до проверки пароля и
// возвращает, сколько ждать (0 — можно проверять).
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

// refundLogin возвращает попытку, которая не дошла до проверки пароля.
func (s *Server) refundLogin(name, ip string) {
	pair := loginPairKey(name, ip)
	first := s.logins.count(pair) <= loginFreeAttempts
	s.logins.refund(pair)
	if first {
		s.loginNames.refund(name)
	}
}

// loginSucceeded снимает счёт этого адреса и потолок имени: хозяин вошёл.
func (s *Server) loginSucceeded(name, ip string) {
	s.logins.succeeded(loginPairKey(name, ip))
	s.loginNames.succeeded(name)
}

// loginForget снимает всё по имени: ссылка сброса пароля.
func (s *Server) loginForget(name string) {
	s.logins.forgetPrefix(name + "\x00")
	s.loginNames.succeeded(name)
}
