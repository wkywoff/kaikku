package api

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogIDs(t *testing.T) {
	cases := []struct {
		id           string
		deezer, mine bool
	}{
		{"3135556", true, false},
		{"k1", false, true},
		{"k1234567", false, true},
		{"k0", false, false},
		{"k01", false, false},
		{"k", false, false},
		{"K1", false, false},
		{"k1x", false, false},
		{"-1", false, false},
		{"", false, false},
		{"k1234567890123456789", false, false},
	}
	for _, c := range cases {
		if got := validDeezerID(c.id); got != c.deezer {
			t.Errorf("validDeezerID(%q) = %v", c.id, got)
		}
		if got := isLocalID(c.id); got != c.mine {
			t.Errorf("isLocalID(%q) = %v", c.id, got)
		}
		if got := validCatalogID(c.id); got != (c.deezer || c.mine) {
			t.Errorf("validCatalogID(%q) = %v", c.id, got)
		}
	}
}

// Свой трек не ставится на скачивание: ни запроса к Deezer, ни job.
func TestEnqueue_RefusesOwnID(t *testing.T) {
	db, _, _, srv := newTestServer(t)
	if _, err := srv.enqueueTrack("", "k5"); !errors.Is(err, errLocalID) {
		t.Fatalf("enqueueTrack(k5) err = %v, want errLocalID", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs`).Scan(&n)
	if n != 0 {
		t.Errorf("%d jobs created", n)
	}
}

// captureLog собирает то, что тест написал в лог.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})
	return &buf
}

// Ответ 500 не раскрывает причину клиенту, но в логе она есть — с запросом,
// без его параметров (в них подписи ссылок и токены).
func TestServerError_LogsCause(t *testing.T) {
	buf := captureLog(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/playlists/42/cover?sig=secret", nil)
	serverError(rec, req, "query failed", errors.New("pq: relation does not exist"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":"query failed"`) || strings.Contains(body, "pq:") {
		t.Errorf("body = %s, want only the message", body)
	}
	if got, want := buf.String(), "PUT /api/playlists/42/cover: query failed: pq: relation does not exist\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// Перевод строки из адреса не становится новой строкой лога.
func TestServerError_PathStaysEncoded(t *testing.T) {
	buf := captureLog(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playlists/a%0Ab", nil)
	serverError(httptest.NewRecorder(), req, "query failed", errors.New("boom"))
	if got := buf.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "a%0Ab") {
		t.Errorf("log = %q, want one line with the path as sent", got)
	}
}

// Человек закрыл страницу, запрос отменился — в лог это не идёт; та же
// ошибка при живом запросе — идёт.
func TestServerError_SkipsClientGone(t *testing.T) {
	buf := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/me/liked", nil).WithContext(ctx)

	serverError(httptest.NewRecorder(), req, "query failed", context.Canceled)
	if buf.Len() == 0 {
		t.Error("a cancelled query of a live request is not logged")
	}
	buf.Reset()
	cancel()
	serverError(httptest.NewRecorder(), req, "query failed", context.Canceled)
	if buf.Len() != 0 {
		t.Errorf("log = %q, want nothing for a request the client dropped", buf.String())
	}
}
