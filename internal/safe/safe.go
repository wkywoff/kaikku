package safe

import (
	"log"
	"runtime/debug"
)

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

func Go(name string, f func()) {
	go Run(name, f)
}
