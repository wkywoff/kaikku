package ingest

import (
	"errors"
	"testing"
)

func TestDiskLow(t *testing.T) {
	t.Cleanup(func(f func(string) (uint64, error)) func() { return func() { freeSpace = f } }(freeSpace))
	for _, c := range []struct {
		free uint64
		err  error
		want bool
	}{{minFreeSpace - 1, nil, true}, {minFreeSpace, nil, false}, {0, errors.New("no stat"), false}} {
		freeSpace = func(string) (uint64, error) { return c.free, c.err }
		if got := DiskLow("/x"); got != c.want {
			t.Errorf("free %d, err %v: %v, want %v", c.free, c.err, got, c.want)
		}
	}
}
