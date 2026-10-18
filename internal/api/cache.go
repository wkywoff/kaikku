package api

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
	"kaikku/internal/flight"
)

// ttlCache — ответы Deezer, которые нет смысла спрашивать на каждый
// запрос: страницы артистов, похожие артисты, дискографии для «Новых
// релизов», чарт. Живёт в памяти API, нулевое значение готово к работе.
type ttlCache struct {
	mu    sync.Mutex
	m     map[string]ttlEntry
	loads flight.Group[any] // промахи с одним ключом в одно время — одной загрузкой
}

type ttlEntry struct {
	v   any
	exp time.Time
}

const ttlCacheMaxEntries = 2000

// storedCached — cached, но промах сначала ищется в кеше Deezer в базе
// (Server.DeezerCache): страницы артистов, дискографии и чарт переживают
// выкатку. Значение из базы живёт в памяти до своего срока в базе.
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

// cached returns the value under key, loading (and remembering for ttl) it
// on a miss. Errors are not cached. Concurrent misses share one load.
func cached[T any](c *ttlCache, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	return cachedUntil(c, key, func() (T, time.Time, error) {
		v, err := load()
		return v, time.Now().Add(ttl), err
	})
}

// cachedUntil — cached, но срок записи называет сам load.
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
	if err != nil || exp.IsZero() { // ошибка или чужая загрузка: она и запомнила
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

// forget drops every entry whose key starts with prefix (the data behind
// it changed: a like changes that person's mixes).
func (c *ttlCache) forget(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}

// Копии закешированных ответов Deezer: overlayFor накладывает правки админа
// прямо на ответ, включая списки исполнителей, а кеш должен оставаться
// ответом самого Deezer.
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
