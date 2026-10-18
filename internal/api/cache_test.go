package api

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Одновременные промахи по одному ключу — одна загрузка, и результат
// запоминается.
func TestCachedSharesLoad(t *testing.T) {
	var c ttlCache
	var loads atomic.Int32
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cached(&c, "k", time.Hour, func() (int, error) {
				loads.Add(1)
				time.Sleep(50 * time.Millisecond)
				return 7, nil
			})
			if v != 7 || err != nil {
				t.Errorf("got %d %v", v, err)
			}
		}()
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Errorf("%d loads, want 1", loads.Load())
	}
	cached(&c, "k", time.Hour, func() (int, error) { loads.Add(1); return 0, nil })
	if loads.Load() != 1 {
		t.Error("the shared result was not remembered")
	}
}
