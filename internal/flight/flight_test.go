package flight

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDo_SharesOneCall(t *testing.T) {
	var g Group[int]
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = g.Do("k", func() (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("%d calls, want 1", calls.Load())
	}
	for _, r := range results {
		if r != 42 {
			t.Errorf("result %d", r)
		}
	}
	// После завершения — новый вызов.
	g.Do("k", func() (int, error) { calls.Add(1); return 0, nil })
	if calls.Load() != 2 {
		t.Error("a finished call was reused")
	}
}

func TestDo_PanicIsAnError(t *testing.T) {
	var g Group[int]
	func() {
		defer func() { recover() }()
		g.Do("k", func() (int, error) { panic("boom") })
	}()
	if _, err := g.Do("k", func() (int, error) { return 0, errors.New("next") }); err == nil || err.Error() != "next" {
		t.Errorf("the key is stuck after a panic: %v", err)
	}
}
