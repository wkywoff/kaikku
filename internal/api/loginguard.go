package api

import (
	"sync"
	"time"
)

// Защита от подбора пароля. Настоящего адреса клиента API может не видеть
// (за прокси все запросы приходят с одного адреса, и лимит nginx на
// /api/login тогда общий на всех).
// Поэтому неудачи считаются по имени пользователя: после
// loginFreeAttempts неудач подряд пауза перед следующей попыткой растёт —
// 30 с, 1, 2, 4, 8 минут, дальше раз в 15 минут: около сотни попыток в
// сутки на имя. Настоящий хозяин имени ждёт не дольше 15 минут, а ссылка
// сброса пароля (handlers_password.go) снимает паузу сразу.

const (
	loginFreeAttempts = 5
	loginPauseFirst   = 30 * time.Second
	loginPauseMax     = 15 * time.Minute
	loginWindow       = 24 * time.Hour // неудачи старше этого забываются
	loginMaxTracked   = 10000
)

type loginGuard struct {
	mu sync.Mutex
	m  map[string]*loginFailures
}

type loginFailures struct {
	count int
	last  time.Time
}

// loginPause — сколько ждать после count неудач подряд.
func loginPause(count int) time.Duration {
	if count < loginFreeAttempts {
		return 0
	}
	d := loginPauseFirst
	for i := loginFreeAttempts; i < count && d < loginPauseMax; i++ {
		d *= 2
	}
	return min(d, loginPauseMax)
}

// wait reports how long name has to wait before the next attempt (0 — go).
func (g *loginGuard) wait(name string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.m[name]
	if f == nil || now.Sub(f.last) > loginWindow {
		return 0
	}
	return max(0, loginPause(f.count)-now.Sub(f.last))
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

func (g *loginGuard) succeeded(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, name)
}

// prune drops forgotten failures; if the map is still full (a flood of
// made-up names), it starts over.
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
		g.m = map[string]*loginFailures{}
	}
}
