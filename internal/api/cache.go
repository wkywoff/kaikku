package api

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/flight"
)

type ttlCache struct {
	mu    sync.Mutex
	m     map[string]ttlEntry
	loads flight.Group[any]
}

type ttlEntry struct {
	v   any
	exp time.Time
}

const ttlCacheMaxEntries = 2000

func storedCached[T any](s *Server, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	return cachedUntil(&s.cache, key, func() (T, time.Time, error) {
		var v T
		if s.DeezerCache != nil {
			if data, expires, ok := s.DeezerCache.Get("api:" + key); ok && json.Unmarshal(data, &v) == nil {
				return v, expires, nil
			}
		}
		v, err := load()
		expires := time.Now().Add(ttl)
		if err == nil && s.DeezerCache != nil {
			if data, err := json.Marshal(v); err == nil {
				s.DeezerCache.Put("api:"+key, data, expires)
			}
		}
		return v, expires, err
	})
}

func cached[T any](c *ttlCache, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	return cachedUntil(c, key, func() (T, time.Time, error) {
		v, err := load()
		return v, time.Now().Add(ttl), err
	})
}

func peekCached[T any](c *ttlCache, key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok && time.Now().Before(e.exp) {
		v, ok := e.v.(T)
		return v, ok
	}
	var zero T
	return zero, false
}

func cachedUntil[T any](c *ttlCache, key string, load func() (T, time.Time, error)) (T, error) {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.m[key]; ok && now.Before(e.exp) {
		c.mu.Unlock()
		return e.v.(T), nil
	}
	c.mu.Unlock()

	var exp time.Time
	r, err := c.loads.Do(key, func() (any, error) {
		v, e, err := load()
		exp = e
		return v, err
	})
	v, _ := r.(T)
	if err != nil || exp.IsZero() {
		return v, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]ttlEntry{}
	}
	if len(c.m) >= ttlCacheMaxEntries {
		for k, e := range c.m {
			if !now.Before(e.exp) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= ttlCacheMaxEntries {
			c.m = map[string]ttlEntry{}
		}
	}
	c.m[key] = ttlEntry{v: v, exp: exp}
	return v, nil
}

func (c *ttlCache) forget(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}

func cloneAlbums(in []deezer.Album) []deezer.Album {
	out := make([]deezer.Album, len(in))
	for i, a := range in {
		a.Contributors = append([]deezer.ArtistRef(nil), a.Contributors...)
		out[i] = a
	}
	return out
}

func cloneTracks(in []deezer.Track) []deezer.Track {
	out := make([]deezer.Track, len(in))
	for i, t := range in {
		t.Contributors = append([]deezer.ArtistRef(nil), t.Contributors...)
		out[i] = t
	}
	return out
}
