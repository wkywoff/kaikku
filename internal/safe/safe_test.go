package safe

import (
	"sync"
	"testing"
)

func TestRun(t *testing.T) {
	ran := false
	if Run("ok", func() { ran = true }) || !ran {
		t.Fatal("a normal run reports no panic")
	}
	if !Run("boom", func() { panic("boom") }) {
		t.Fatal("a panic is caught and reported")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	Go("goroutine", func() { defer wg.Done(); panic("in a goroutine") })
	wg.Wait() // процесс жив — тест дошёл сюда
}
