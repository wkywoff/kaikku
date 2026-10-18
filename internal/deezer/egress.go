package deezer

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Выходы к Deezer. Лимит у Deezer — на IP, поэтому второй выход (прокси с
// другим IP, например WARP как локальный SOCKS5) удваивает его: запрос
// идёт через тот, где есть место. Прокси не ответил — запрос повторяется
// напрямую, а прокси отдыхает: минуту, после повторного сбоя — вдвое
// дольше, до 10 минут. После паузы через него идёт один пробный запрос,
// остальные — напрямую, пока проба не пройдёт: у WARP часто открыт порт при
// мёртвом туннеле, и иначе раз в минуту десятки запросов ждали бы таймаута.

type egress struct {
	name    string
	http    *http.Client
	limiter *rateLimiter // nil — без ограничения (тесты)
	proxy   bool

	mu        sync.Mutex
	fails     int       // сбоев подряд; 0 — работает
	downUntil time.Time // пауза после сбоя
	probing   bool      // пробный запрос в пути
}

var proxyDownFor = time.Minute // первая пауза; переменная для тестов

const proxyDownMax = 10 * time.Minute

// up — выход работает (не на паузе и не на пробе).
func (e *egress) up() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fails == 0
}

// usable — через выход можно пойти сейчас: он работает или пауза прошла и
// пробы ещё нет.
func (e *egress) usable() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fails == 0 || !e.probing && !time.Now().Before(e.downUntil)
}

// claim — занять выход под запрос; после паузы это проба, и она одна.
func (e *egress) claim() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fails == 0 {
		return true
	}
	if e.probing || time.Now().Before(e.downUntil) {
		return false
	}
	e.probing = true
	return true
}

// done — итог запроса через прокси: err без ответа Deezer — сбой.
func (e *egress) done(err error) {
	var apiErr *APIError
	failed := err != nil && !errors.As(err, &apiErr)
	e.mu.Lock()
	e.probing = false
	was := e.fails
	if !failed {
		e.fails = 0
		e.mu.Unlock()
		if was > 0 {
			log.Printf("deezer: %s is back", e.name)
		}
		return
	}
	e.fails++
	pause := proxyDownFor << min(e.fails-1, 10)
	pause = min(pause, proxyDownMax)
	e.downUntil = time.Now().Add(pause)
	e.mu.Unlock()
	log.Printf("deezer: %s failed, going direct for %s: %v", e.name, pause, err)
}

// WithProxy добавляет выход через прокси (socks5://… или http://…) со своим
// лимитом n запросов за per.
func (c *Client) WithProxy(proxy *url.URL, n int, per time.Duration) *Client {
	c.egresses = append(c.egresses, &egress{
		name: "proxy " + proxy.Host,
		// Deezer отвечает за доли секунды; 5 с — уже признак мёртвого туннеля.
		http: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy), TLSHandshakeTimeout: 3 * time.Second,
		}},
		limiter: newRateLimiter(n, per),
		proxy:   true,
	})
	return c
}

// Capacity — сколько запросов за 5 с пропускают работающие выходы вместе
// (0 — без ограничения). Прокси на паузе или на пробе не считается.
func (c *Client) Capacity() int {
	total := 0
	for _, e := range c.egresses {
		if e.limiter == nil {
			return 0
		}
		if e.up() {
			total += int(int64(e.limiter.n) * int64(5*time.Second) / int64(e.limiter.per))
		}
	}
	return total
}

// pick — выход для запроса: первый со свободным местом (avoid — нет), а
// если места нет нигде — ждёт у того, где оно освободится раньше.
// Возвращает и сколько ждал.
func (c *Client) pick(avoid *egress) (*egress, time.Duration) {
	start := time.Now()
	for {
		var next *egress
		var nextIn time.Duration
		for _, e := range c.egresses {
			if e == avoid || !e.usable() {
				continue
			}
			if e.limiter == nil {
				if e.claim() {
					return e, time.Since(start)
				}
				continue
			}
			ok, in := e.limiter.tryTake()
			if ok && e.claim() {
				return e, time.Since(start)
			}
			if ok {
				continue // пробу уже забрал другой запрос
			}
			if next == nil || in < nextIn {
				next, nextIn = e, in
			}
		}
		if next == nil { // так не бывает: прямой выход не пропускается
			return c.egresses[0], time.Since(start)
		}
		time.Sleep(nextIn)
	}
}

// errTransport — запрос не дошёл до Deezer (соединение, прокси, таймаут).
type errTransport struct{ err error }

func (e *errTransport) Error() string { return "request: " + e.err.Error() }
func (e *errTransport) Unwrap() error { return e.err }
