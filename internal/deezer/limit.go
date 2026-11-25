package deezer

import (
	"context"
	"log"
	"sync"
	"time"
)

type rateLimiter struct {
	mu    sync.Mutex
	n     int
	per   time.Duration
	times []time.Time
}

func newRateLimiter(n int, per time.Duration) *rateLimiter {
	return &rateLimiter{n: n, per: per}
}

func (l *rateLimiter) wait() time.Duration {
	d, _ := l.waitContext(context.Background())
	return d
}

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

type Limiter struct{ l *rateLimiter }

func NewLimiter(n int, per time.Duration) *Limiter { return &Limiter{newRateLimiter(n, per)} }

func (l *Limiter) Wait() { l.l.wait() }

func (l *Limiter) WaitContext(ctx context.Context) error {
	_, err := l.l.waitContext(ctx)
	return err
}

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
