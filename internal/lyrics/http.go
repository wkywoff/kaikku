package lyrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36"

var httpClient = &http.Client{Timeout: 10 * time.Second}

// statusError — ответ не 2xx.
type statusError struct {
	Code int
	Body string
}

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d: %.200s", e.Code, e.Body) }

// fetch делает GET и отдаёт тело ответа (до 8 МБ).
func fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	return do(ctx, http.MethodGet, url, headers, nil)
}

// postJSON отправляет body как JSON и разбирает JSON-ответ в out.
func postJSON(ctx context.Context, url string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := do(ctx, http.MethodPost, url, h, b)
	if err != nil {
		return err
	}
	return json.Unmarshal(resp, out)
}

// retryAfter — пауза перед единственным повтором, когда источник занят
// (LRCLIB под нагрузкой отвечает 503 «retry in a moment»).
var retryAfter = 700 * time.Millisecond

func do(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) ([]byte, error) {
	b, err := doOnce(ctx, method, rawURL, headers, body)
	var se *statusError
	if errors.As(err, &se) && (se.Code == http.StatusTooManyRequests || se.Code >= 500) {
		select {
		case <-time.After(retryAfter):
			return doOnce(ctx, method, rawURL, headers, body)
		case <-ctx.Done():
		}
	}
	return b, err
}

func doOnce(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) ([]byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// В ошибку и лог — только хост и путь, без параметров запроса.
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, fmt.Errorf("%s %s%s: %w", ue.Op, req.URL.Host, req.URL.Path, ue.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{Code: resp.StatusCode, Body: string(b)}
	}
	return b, nil
}

func fetchJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	b, err := fetch(ctx, url, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
