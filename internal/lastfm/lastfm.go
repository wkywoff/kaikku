// Package lastfm — скробблинг в Last.fm (https://www.last.fm/api):
// привязка аккаунта (веб-авторизация → ключ сессии), «сейчас играет»,
// скробблы пачками и ♥; описания и теги артистов. Подписывает запросы
// общим секретом приложения.
package lastfm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	apiRoot  = "https://ws.audioscrobbler.com/2.0/"
	authPage = "https://www.last.fm/api/auth/"
	// MaxBatch — больше скробблов за один запрос Last.fm не принимает.
	MaxBatch = 50
)

type Client struct {
	Key, Secret string
	BaseURL     string // пусто — настоящий Last.fm; тесты подставляют свой
	HTTP        *http.Client
}

// New — клиент с ключом и секретом приложения; без них nil (скробблинг выключен).
func New(key, secret string) *Client {
	if key == "" || secret == "" {
		return nil
	}
	return &Client{Key: key, Secret: secret, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// AuthURL — страница Last.fm «Разрешить доступ»; после неё Last.fm
// возвращает человека на cb с ?token=… (пусто — на адрес из настроек
// приложения на Last.fm).
func (c *Client) AuthURL(cb string) string {
	v := url.Values{"api_key": {c.Key}}
	if cb != "" {
		v.Set("cb", cb)
	}
	return authPage + "?" + v.Encode()
}

// Track — то, что Last.fm знает о прослушивании.
type Track struct {
	Artist   string
	Title    string
	Album    string
	Duration int       // секунды; 0 — неизвестно
	Started  time.Time // начало прослушивания (для скроббла)
}

// Error — ошибка, которую вернул сам Last.fm (коды: last.fm/api/errorcodes).
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("last.fm error %d: %s", e.Code, e.Message) }

// SessionInvalid: ключ сессии отозван (человек убрал доступ на Last.fm) —
// привязку пора удалить.
func SessionInvalid(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == 9
}

// Retryable: Last.fm или сеть временно недоступны — повторить позже.
// Остальные ошибки (неверные данные) повтором не исправить.
func Retryable(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		switch e.Code {
		case 8, 11, 16, 29: // сбой, сервис недоступен, временная ошибка, лимит
			return true
		}
		return false
	}
	return err != nil
}

// Session меняет токен из веб-авторизации на ключ сессии (бессрочный).
func (c *Client) Session(ctx context.Context, token string) (name, key string, err error) {
	var r struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := c.call(ctx, "auth.getSession", url.Values{"token": {token}}, &r); err != nil {
		return "", "", err
	}
	if r.Session.Key == "" {
		return "", "", errors.New("last.fm: empty session")
	}
	return r.Session.Name, r.Session.Key, nil
}

func trackParams(t Track, suffix string) url.Values {
	v := url.Values{"artist" + suffix: {t.Artist}, "track" + suffix: {t.Title}}
	if t.Album != "" {
		v.Set("album"+suffix, t.Album)
	}
	if t.Duration > 0 {
		v.Set("duration"+suffix, strconv.Itoa(t.Duration))
	}
	return v
}

// NowPlaying — «сейчас слушает» в профиле (не скроббл).
func (c *Client) NowPlaying(ctx context.Context, sk string, t Track) error {
	v := trackParams(t, "")
	v.Set("sk", sk)
	return c.call(ctx, "track.updateNowPlaying", v, nil)
}

// Scrobble отправляет до MaxBatch прослушиваний одним запросом.
func (c *Client) Scrobble(ctx context.Context, sk string, ts []Track) error {
	if len(ts) == 0 || len(ts) > MaxBatch {
		return fmt.Errorf("last.fm: %d scrobbles in a batch", len(ts))
	}
	v := url.Values{"sk": {sk}}
	for i, t := range ts {
		suffix := "[" + strconv.Itoa(i) + "]"
		for k, vals := range trackParams(t, suffix) {
			v[k] = vals
		}
		v.Set("timestamp"+suffix, strconv.FormatInt(t.Started.Unix(), 10))
	}
	return c.call(ctx, "track.scrobble", v, nil)
}

// Love ставит (on) или снимает ♥ у трека.
func (c *Client) Love(ctx context.Context, sk string, t Track, on bool) error {
	method := "track.unlove"
	if on {
		method = "track.love"
	}
	return c.call(ctx, method, url.Values{"sk": {sk}, "artist": {t.Artist}, "track": {t.Title}}, nil)
}

// ArtistBio — описание артиста с Last.fm (пишут сами слушатели, лицензия
// CC BY-SA: показывать со ссылкой url) на языке lang, а если на нём нет —
// на английском. Ищется по имени, без автоисправления: «похожее» имя —
// чаще всего другой артист. Нет такого артиста — "" без ошибки. HTML как
// есть, с хвостом «Read more on Last.fm».
func (c *Client) ArtistBio(ctx context.Context, name, lang string) (bio, link string, err error) {
	var r struct {
		Artist struct {
			URL string `json:"url"`
			Bio struct {
				Content string `json:"content"`
			} `json:"bio"`
		} `json:"artist"`
	}
	err = c.get(ctx, "artist.getInfo", url.Values{"artist": {name}, "lang": {lang}, "autocorrect": {"0"}}, &r)
	var e *Error
	if errors.As(err, &e) && e.Code == 6 { // artist not found
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return r.Artist.Bio.Content, r.Artist.URL, nil
}

// Tag — тег артиста на Last.fm (жанры и всё, что ставят слушатели);
// Count — вес тега у этого артиста, 0–100.
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ArtistTopTags — теги артиста по убыванию веса. Ищется по имени, как
// ArtistBio. Нет такого артиста — пусто без ошибки.
func (c *Client) ArtistTopTags(ctx context.Context, name string) ([]Tag, error) {
	var r struct {
		TopTags struct {
			Tag []Tag `json:"tag"`
		} `json:"toptags"`
	}
	err := c.get(ctx, "artist.getTopTags", url.Values{"artist": {name}, "autocorrect": {"0"}}, &r)
	var e *Error
	if errors.As(err, &e) && e.Code == 6 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.TopTags.Tag, nil
}

// sign — api_sig: параметры по алфавиту «имязначение» подряд, затем секрет, md5.
func (c *Client) sign(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k != "format" && k != "callback" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(v.Get(k))
	}
	b.WriteString(c.Secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// call — подписанный POST; out == nil — ответ не нужен, только ошибка.
func (c *Client) call(ctx context.Context, method string, v url.Values, out any) error {
	v.Set("method", method)
	v.Set("api_key", c.Key)
	v.Set("api_sig", c.sign(v))
	v.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.root(), strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, out)
}

// get — чтение без подписи и сессии (сведения об артисте и т.п.).
func (c *Client) get(ctx context.Context, method string, v url.Values, out any) error {
	v.Set("method", method)
	v.Set("api_key", c.Key)
	v.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.root()+"?"+v.Encode(), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) root() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return apiRoot
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", "kaikku (https://github.com/wkywoff/kaikku)")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var e struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != 0 {
		return &Error{Code: e.Error, Message: e.Message}
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("last.fm: HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}
