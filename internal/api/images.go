package api

import (
	"container/list"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Обложки и фото артистов из каталога Deezer браузер берёт не с
// cdn-images.dzcdn.net, а через нас (/api/img/...): у части людей CDN
// Deezer недоступен (блокировки, «белые списки» мобильного интернета),
// а наш домен открывается. Фронт сам переписывает такие ссылки в ответах
// API (frontend/src/lib/api.js).
//
// Без авторизации — <img> не шлёт Bearer, а картинки и так публичные.
// Открытым прокси это не становится: пропускаются только пути вида
// images/<cover|artist|misc>/<md5>/<WxH>-….jpg на одном хосте.

var deezerImageHost = "https://cdn-images.dzcdn.net/images/"

var (
	imageKindRe = regexp.MustCompile(`^(cover|artist|misc|playlist|user)$`)
	imageHashRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	imageFileRe = regexp.MustCompile(`^([0-9]{2,4})x([0-9]{2,4})-[0-9a-z.-]{1,40}\.(jpg|png)$`)
)

// Квадраты, которые приходят в ответах Deezer (56/250/500/1000) и которые
// просит фронт (imageproxy.js → sized, srcsetOf: 64/200/300/320/640). Другие
// размеры не нужны — и через нас нельзя гонять картинки Deezer любого размера.
var imageSizes = map[string]bool{"56": true, "64": true, "200": true, "250": true, "300": true, "320": true, "500": true, "640": true, "1000": true}

var imageClient = &http.Client{Timeout: 20 * time.Second}

func (s *Server) handleDeezerImage(w http.ResponseWriter, r *http.Request) {
	kind, hash, file := r.PathValue("kind"), r.PathValue("hash"), r.PathValue("file")
	size := imageFileRe.FindStringSubmatch(file)
	if !imageKindRe.MatchString(kind) || !imageHashRe.MatchString(hash) || size == nil || size[1] != size[2] || !imageSizes[size[1]] {
		http.NotFound(w, r)
		return
	}
	key := kind + "/" + hash + "/" + file
	img, ok := imageCache.get(key)
	if !ok {
		var err error
		if img, err = fetchDeezerImage(r.Context(), key); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		imageCache.put(key, img)
	}
	w.Header().Set("Content-Type", img.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(img.data)))
	// Хеш в пути — от содержимого картинки, так что её можно кешировать навсегда.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(img.data)
}

// Картинка больше этого — не обложка.
const maxImageBytes = 4 << 20

func fetchDeezerImage(ctx context.Context, path string) (cachedImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deezerImageHost+path, nil)
	if err != nil {
		return cachedImage{}, errors.New("bad image path")
	}
	resp, err := imageClient.Do(req)
	if err != nil {
		return cachedImage{}, errors.New("image fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cachedImage{}, errors.New("image not available")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return cachedImage{}, errors.New("image fetch failed")
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "image/jpeg" && ct != "image/png" {
		ct = "image/jpeg"
	}
	return cachedImage{contentType: ct, data: data}, nil
}

// imageCache — последние картинки Deezer в памяти сервера. Приложения
// (Subsonic) просят одни и те же обложки снова и снова, а адрес запроса у
// них каждый раз свой (соль входа в параметрах) — их кеш не помогает, и
// без этого каждая обложка шла бы заново с CDN Deezer.
var imageCache = &byteCache{max: 48 << 20}

type cachedImage struct {
	contentType string
	data        []byte
}

// byteCache — LRU по суммарному размеру.
type byteCache struct {
	max int

	mu    sync.Mutex
	size  int
	order list.List // *list.Element.Value — *byteCacheEntry, свежие спереди
	m     map[string]*list.Element
}

type byteCacheEntry struct {
	key string
	img cachedImage
}

func (c *byteCache) get(key string) (cachedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return cachedImage{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*byteCacheEntry).img, true
}

func (c *byteCache) put(key string, img cachedImage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*list.Element{}
	}
	if _, ok := c.m[key]; ok || len(img.data) > c.max {
		return
	}
	c.m[key] = c.order.PushFront(&byteCacheEntry{key, img})
	c.size += len(img.data)
	for c.size > c.max {
		last := c.order.Back()
		e := last.Value.(*byteCacheEntry)
		c.order.Remove(last)
		delete(c.m, e.key)
		c.size -= len(e.img.data)
	}
}
