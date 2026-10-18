package deezer

import (
	"context"
	"log"
	"sync"
	"time"
)

// Deezer пускает не больше 50 запросов за 5 секунд с одного IP — общих для
// API и воркера, они на одном сервере. Сверх лимита он отвечает ошибкой
// с кодом 4 (HTTP 200, см. APIError). Поэтому каждый клиент сам держит
// свою долю лимита (rateLimiter), а если Deezer всё же ответил «квота» —
// ждёт и повторяет запрос (Client.get).

// rateLimiter allows at most n requests in any window of length per.
type rateLimiter struct {
	mu    sync.Mutex
	n     int
	per   time.Duration
	times []time.Time // моменты последних запросов, по возрастанию, не больше n
}

func newRateLimiter(n int, per time.Duration) *rateLimiter {
	return &rateLimiter{n: n, per: per}
}

// wait blocks until one more request fits into the window, then records
// it; returns how long it waited.
func (l *rateLimiter) wait() time.Duration {
	d, _ := l.waitContext(context.Background())
	return d
}

// tryTake takes a slot if one is free now; otherwise reports how soon one
// frees up.
func (l *rateLimiter) tryTake() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	old := 0
	for old < len(l.times) && now.Sub(l.times[old]) >= l.per {
		old++
	}
	l.times = l.times[old:]
	if len(l.times) < l.n {
		l.times = append(l.times, now)
		return true, 0
	}
	return false, l.per - now.Sub(l.times[0])
}

// waitContext — wait, but gives up (without taking a slot) when ctx ends.
func (l *rateLimiter) waitContext(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	for {
		l.mu.Lock()
		now := time.Now()
		old := 0
		for old < len(l.times) && now.Sub(l.times[old]) >= l.per {
			old++
		}
		l.times = l.times[old:]
		if len(l.times) < l.n {
			l.times = append(l.times, now)
			l.mu.Unlock()
			return now.Sub(start), nil
		}
		delay := l.per - now.Sub(l.times[0])
		l.mu.Unlock()
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return time.Since(start), ctx.Err()
		case <-t.C:
		}
	}
}

// Limiter is a rate limit for callers outside the package: a bulk job (the
// playlist import) takes its own, smaller share so that page loads, which
// go through the client's limiter, aren't stuck behind it.
type Limiter struct{ l *rateLimiter }

func NewLimiter(n int, per time.Duration) *Limiter { return &Limiter{newRateLimiter(n, per)} }

// Wait blocks until one more request fits into the window.
func (l *Limiter) Wait() { l.l.wait() }

// WaitContext — Wait, but returns ctx's error if ctx ends first.
func (l *Limiter) WaitContext(ctx context.Context) error {
	_, err := l.l.waitContext(ctx)
	return err
}

// limitStats — в журнал: сколько запросов заметно ждали своей доли лимита
// и сколько раз Deezer всё же ответил «квота». Сводкой раз в
// limitLogEvery, а не строкой на запрос.
type limitStats struct {
	mu        sync.Mutex
	count     int
	longest   time.Duration
	quota     int
	scheduled bool
}

const (
	limitSlowWait = 500 * time.Millisecond
	limitLogEvery = 30 * time.Second
)

func (s *limitStats) waited(d time.Duration) {
	if d < limitSlowWait {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	s.longest = max(s.longest, d)
	s.schedule()
}

func (s *limitStats) overQuota() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quota++
	s.schedule()
}

// schedule — сводка через limitLogEvery (s.mu уже взят).
func (s *limitStats) schedule() {
	if s.scheduled {
		return
	}
	s.scheduled = true
	time.AfterFunc(limitLogEvery, s.flush)
}

func (s *limitStats) flush() {
	s.mu.Lock()
	count, longest, quota := s.count, s.longest, s.quota
	s.count, s.longest, s.quota, s.scheduled = 0, 0, 0, false
	s.mu.Unlock()
	log.Printf("deezer: %d request(s) waited for the rate limit (longest %.1fs), %d over-quota answer(s), in the last %s",
		count, longest.Seconds(), quota, limitLogEvery)
}
