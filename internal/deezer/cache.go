package deezer

import (
	"sync"
	"time"
)

const (
	trackCacheTTL     = 30 * 24 * time.Hour
	albumCacheTTL     = 7 * 24 * time.Hour
	tracklistCacheTTL = 7 * 24 * time.Hour
	freshCacheTTL     = 3 * time.Hour
	freshReleaseAge   = 14 * 24 * time.Hour
	cacheMaxEntries   = 20000
)

func freshRelease(date string) bool {
	d, err := time.Parse("2006-01-02", date)
	return err == nil && time.Since(d) < freshReleaseAge
}

type ttlCache[T any] struct {
	clone  func(T) T
	prefix string
	store  Store

	mu sync.Mutex
	m  map[string]cacheEntry[T]
}

type cacheEntry[T any] struct {
	v       T
	expires time.Time
}

func (c *ttlCache[T]) get(id string) (T, bool) {
	c.mu.Lock()
	e, ok := c.m[id]
	c.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return c.clone(e.v), true
	}
	return c.fromStore(id)
}

func (c *ttlCache[T]) has(id string) bool {
	c.mu.Lock()
	e, ok := c.m[id]
	c.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return true
	}
	_, ok = c.fromStore(id)
	return ok
}

func (c *ttlCache[T]) putFor(id string, v T, ttl time.Duration) {
	expires := time.Now().Add(ttl)
	c.remember(id, v, expires)
	c.toStore(id, v, expires)
}

func (c *ttlCache[T]) remember(id string, v T, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= cacheMaxEntries {
		c.m = map[string]cacheEntry[T]{}
	}
	c.m[id] = cacheEntry[T]{v: c.clone(v), expires: expires}
}

func cloneTrack(t Track) Track {
	t.Contributors = append([]ArtistRef(nil), t.Contributors...)
	return t
}

func cloneTracks(ts []Track) []Track {
	out := make([]Track, len(ts))
	for i, t := range ts {
		out[i] = cloneTrack(t)
	}
	return out
}

func cloneAlbum(a Album) Album {
	a.Contributors = append([]ArtistRef(nil), a.Contributors...)
	return a
}
