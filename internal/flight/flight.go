// Package flight — одновременные одинаковые загрузки одним вызовом: при
// старте трека один и тот же трек Deezer спрашивают плеер, история, тексты
// и лайк, и без этого каждый промах кеша шёл к Deezer сам.
package flight

import (
	"errors"
	"sync"
)

type Group[T any] struct {
	mu sync.Mutex
	m  map[string]*call[T]
}

type call[T any] struct {
	done chan struct{}
	v    T
	err  error
}

var errPanicked = errors.New("flight: load panicked")

// Do вызывает fn для key; кто пришёл с тем же key, пока она идёт, ждёт её
// и получает тот же результат (общий: менять его на месте нельзя).
func (g *Group[T]) Do(key string, fn func() (T, error)) (T, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.v, c.err
	}
	if g.m == nil {
		g.m = map[string]*call[T]{}
	}
	c := &call[T]{done: make(chan struct{}), err: errPanicked}
	g.m[key] = c
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		close(c.done)
	}()
	c.v, c.err = fn()
	return c.v, c.err
}
