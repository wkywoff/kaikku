package deezer

import (
	"context"
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
// дольше, до 10 минут (если он единственный открытый — 10 секунд). После паузы
// через него идёт один пробный запрос, остальные — напрямую, пока проба не
// пройдёт: у WARP часто открыт порт при мёртвом туннеле, и иначе раз в минуту
// десятки запросов ждали бы таймаута.
//
// Ещё Deezer смотрит на страну IP: там, где он не работает (/infos отвечает
// open:false), списки приходят пустыми, с виду как настоящие — {"data":[],
// "total":0} — и попали бы в кеш как «у артиста нет релизов». Через такой
// выход списки не идут: закрыты все — запрос кончается ошибкой, а она не
// кешируется. Отдельные альбом, трек и артист и треклист альбома Deezer
// отдаёт и туда (разница — поля, которых мы не берём: readable, preview,
// nb_album), поэтому они идут через закрытый выход, если другого нет
// (getObject).

// regionCheckEvery — как часто спрашивать /infos. Страна IP меняется редко,
// но на это время пустые ответы успеют попасть в кеш, так что не реже.
const regionCheckEvery = 5 * time.Minute

// errNoEgress — ни одного выхода, через который Deezer сейчас ответит;
// errProxyPaused — то же, но ненадолго: прокси открыт и после сбоя отдыхает.
var (
	errNoEgress    = errors.New("deezer: no open way out")
	errProxyPaused = errors.New("deezer: proxy is paused after a failure and direct is closed")
)

// unavailable — почему идти не через что.
func (c *Client) unavailable() error {
	if c.openOther(nil) {
		return errProxyPaused
	}
	return errNoEgress
}

type egress struct {
	name    string
	http    *http.Client
	limiter *rateLimiter // nil — без ограничения (тесты)
	proxy   bool

	mu        sync.Mutex
	fails     int       // сбоев подряд; 0 — работает
	downUntil time.Time // пауза после сбоя
	probing   bool      // пробный запрос в пути
	closed    bool      // Deezer не работает в стране этого IP
}

var (
	proxyDownFor        = time.Minute      // первая пауза; переменные для тестов
	proxyOnlyWayDownFor = 10 * time.Second // пауза, когда других открытых выходов нет
)

const proxyDownMax = 10 * time.Minute

// up — выход работает (не закрыт, не на паузе и не на пробе).
func (e *egress) up() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fails == 0 && !e.closed
}

// usable — через выход можно пойти сейчас: он работает или пауза прошла и
// пробы ещё нет. Закрытый годится только с closedToo.
func (e *egress) usable(closedToo bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed && !closedToo {
		return false
	}
	return e.fails == 0 || !e.probing && !time.Now().Before(e.downUntil)
}

func (e *egress) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// setClosed запоминает ответ /infos; в лог — только когда он изменился.
func (e *egress) setClosed(closed bool, country string) {
	e.mu.Lock()
	was := e.closed
	e.closed = closed
	e.mu.Unlock()
	switch {
	case closed && !was:
		log.Printf("deezer: %s is closed, Deezer sees it in %q and withholds lists from it", e.name, country)
	case !closed && was:
		log.Printf("deezer: %s is open again (%q)", e.name, country)
	}
}

// WatchRegions узнаёт, какие выходы открыты, и дальше проверяет их раз в
// regionCheckEvery. Первая проверка — до возврата: иначе первые запросы
// могут успеть уйти через закрытый выход и закешировать пустоту.
func (c *Client) WatchRegions(ctx context.Context) {
	c.checkRegions()
	go func() {
		t := time.NewTicker(regionCheckEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.checkRegions()
			}
		}
	}()
}

func (c *Client) checkRegions() {
	wasOpen := c.openOther(nil)
	defer func() {
		if wasOpen && !c.openOther(nil) {
			log.Printf("deezer: no open way out: discographies, top tracks and search will fail, single albums and tracks still work; set DEEZER_PROXY to a proxy in another country")
		}
	}()
	var wg sync.WaitGroup
	for _, e := range c.egresses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.checkRegion(e)
		}()
	}
	wg.Wait()
}

// checkRegion: нет ответа или он непонятный — оставляем как было. За сбоями
// соединения следит сам get, а один запрос в 5 минут лимита не касается.
func (c *Client) checkRegion(e *egress) {
	var info struct {
		Country string `json:"country_iso"`
		Open    *bool  `json:"open"`
	}
	if err := c.getOnce(e, "/infos", &info); err != nil || info.Open == nil {
		return
	}
	e.setClosed(!*info.Open, info.Country)
}

// openOther — есть ли открытый выход, кроме e (nil — любой открытый).
func (c *Client) openOther(e *egress) bool {
	for _, o := range c.egresses {
		if o != e && !o.isClosed() {
			return true
		}
	}
	return false
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

// done — итог запроса через прокси: err без ответа Deezer — сбой. Запросы,
// ушедшие вместе (поиск шлёт сразу несколько), падают вместе: пока идёт пауза,
// новые сбои её не удлиняют. maxPause — потолок паузы (pauseLimit).
func (e *egress) done(err error, maxPause time.Duration) {
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
	if was > 0 && time.Now().Before(e.downUntil) {
		e.mu.Unlock()
		return
	}
	e.fails++
	pause := proxyDownFor << min(e.fails-1, 10)
	pause = min(pause, maxPause)
	e.downUntil = time.Now().Add(pause)
	e.mu.Unlock()
	log.Printf("deezer: %s failed, not using it for %s: %v", e.name, pause, err)
}

// pauseLimit — самая долгая пауза прокси e. Других открытых выходов нет — без
// него не работают списки, и лучше пробовать каждые 10 секунд: мёртвый туннель
// задержит только один запрос, остальные сразу получат ошибку.
func (c *Client) pauseLimit(e *egress) time.Duration {
	if c.openOther(e) {
		return proxyDownMax
	}
	return proxyOnlyWayDownFor
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
// Возвращает и сколько ждал; nil — идти не через что (прямой выход закрыт, а
// прокси на паузе или его нет). closedToo — закрытые выходы тоже годятся.
func (c *Client) pick(avoid *egress, closedToo bool) (*egress, time.Duration) {
	start := time.Now()
	for {
		var next *egress
		var nextIn time.Duration
		for _, e := range c.egresses {
			if e == avoid || !e.usable(closedToo) {
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
		if next == nil {
			return nil, time.Since(start)
		}
		time.Sleep(nextIn)
	}
}

// errTransport — запрос не дошёл до Deezer (соединение, прокси, таймаут).
type errTransport struct{ err error }

func (e *errTransport) Error() string { return "request: " + e.err.Error() }
func (e *errTransport) Unwrap() error { return e.err }
