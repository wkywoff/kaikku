package deezer

import (
	"sync"
	"time"
)

// Треки, альбомы и треклисты альбомов Deezer почти не меняются, а
// спрашивают их часто: один и тот же трек — при старте в плеере (все
// исполнители), при записи в историю, при лайке, при догрузке фитов для
// треклиста. Кеш убирает эти повторы и бережёт лимит запросов (limit.go);
// со store (store.go) он переживает и перезапуск.
//
// Значения отдаются копиями (clone): вызывающие правят полученное на месте
// (например, ручные правки админа поверх ответа Deezer), а кеш должен
// оставаться ответом самого Deezer.

const (
	trackCacheTTL     = 30 * 24 * time.Hour // исполнители и ISRC трека почти не меняются
	albumCacheTTL     = 7 * 24 * time.Hour
	tracklistCacheTTL = 7 * 24 * time.Hour
	// Релиз вышел меньше двух недель назад или ещё не вышел, либо в
	// треклисте меньше треков, чем в альбоме: до выхода Deezer показывает
	// пару треков, и неполный треклист держался бы неделю.
	freshCacheTTL   = 3 * time.Hour
	freshReleaseAge = 14 * 24 * time.Hour
	cacheMaxEntries = 20000
)

// freshRelease — релиз с датой date (ГГГГ-ММ-ДД) ещё не вышел или вышел
// меньше freshReleaseAge назад.
func freshRelease(date string) bool {
	d, err := time.Parse("2006-01-02", date)
	return err == nil && time.Since(d) < freshReleaseAge
}

type ttlCache[T any] struct {
	ttl    time.Duration
	clone  func(T) T
	prefix string // ключ в store: "track:" и т. п.
	store  Store  // nil — только память

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

// has — есть ли свежая запись (из store она заодно попадает в память).
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

func (c *ttlCache[T]) put(id string, v T) { c.putFor(id, v, c.ttl) }

// putFor — put со своим сроком.
func (c *ttlCache[T]) putFor(id string, v T, ttl time.Duration) {
	expires := time.Now().Add(ttl)
	c.remember(id, v, expires)
	c.toStore(id, v, expires)
}

// remember — запись только в память.
func (c *ttlCache[T]) remember(id string, v T, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= cacheMaxEntries { // простая защита от роста: сбросить целиком
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
