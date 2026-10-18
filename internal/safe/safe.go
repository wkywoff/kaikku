// Package safe — запуск фоновой работы так, чтобы паника в ней не роняла
// весь процесс: net/http ловит паники только в горутине самого запроса, а
// всё, что запущено через go (фоновое скачивание, Last.fm, теги, воркер),
// без recover убивает API или воркер целиком.
package safe

import (
	"log"
	"runtime/debug"
)

// Run выполняет f; паника — в лог со стеком, и true.
func Run(name string, f func()) (panicked bool) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic in %s: %v\n%s", name, p, debug.Stack())
			panicked = true
		}
	}()
	f()
	return false
}

// Go запускает f в своей горутине под Run.
func Go(name string, f func()) {
	go Run(name, f)
}
