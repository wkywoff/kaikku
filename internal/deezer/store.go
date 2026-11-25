package deezer

import (
	"encoding/json"
	"strings"
	"time"
)

type Store interface {
	Get(key string) (data []byte, expires time.Time, ok bool)
	Put(key string, data []byte, expires time.Time)
	Forget(key string)
}

type batchStore interface {
	GetMany(keys []string) map[string]StoreEntry
}

type StoreEntry struct {
	Data    []byte
	Expires time.Time
}

func (c *Client) WithStore(st Store) *Client {
	c.tracks.store, c.albums.store, c.albumTracks.store = st, st, st
	return c
}

func (c *Client) ForgetAlbum(id string) {
	c.albums.forget(id)
	c.albumTracks.forget(id)
}

func (c *ttlCache[T]) forget(id string) {
	c.mu.Lock()
	for k := range c.m {
		if k == id || strings.HasPrefix(k, id+":") {
			delete(c.m, k)
		}
	}
	c.mu.Unlock()
	if c.store != nil {
		c.store.Forget(c.prefix + id)
	}
}

func (c *ttlCache[T]) fromStore(id string) (T, bool) {
	var v T
	if c.store == nil {
		return v, false
	}
	data, expires, ok := c.store.Get(c.prefix + id)
	if !ok || json.Unmarshal(data, &v) != nil {
		return v, false
	}
	c.remember(id, v, expires)
	return c.clone(v), true
}

func (c *ttlCache[T]) getMany(ids []string) map[string]T {
	out := make(map[string]T, len(ids))
	var miss []string
	now := time.Now()
	c.mu.Lock()
	for _, id := range ids {
		if e, ok := c.m[id]; ok && now.Before(e.expires) {
			out[id] = c.clone(e.v)
		} else {
			miss = append(miss, id)
		}
	}
	c.mu.Unlock()
	if len(miss) == 0 || c.store == nil {
		return out
	}
	bs, ok := c.store.(batchStore)
	if !ok {
		for _, id := range miss {
			if v, ok := c.fromStore(id); ok {
				out[id] = v
			}
		}
		return out
	}
	keys := make([]string, len(miss))
	for i, id := range miss {
		keys[i] = c.prefix + id
	}
	for key, e := range bs.GetMany(keys) {
		var v T
		if json.Unmarshal(e.Data, &v) != nil {
			continue
		}
		id := strings.TrimPrefix(key, c.prefix)
		c.remember(id, v, e.Expires)
		out[id] = c.clone(v)
	}
	return out
}

func (c *ttlCache[T]) toStore(id string, v T, expires time.Time) {
	if c.store == nil {
		return
	}
	if data, err := json.Marshal(v); err == nil {
		c.store.Put(c.prefix+id, data, expires)
	}
}
