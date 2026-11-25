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

func TestServerError_PathStaysEncoded(t *testing.T) {
	buf := captureLog(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playlists/a%0Ab", nil)
	serverError(httptest.NewRecorder(), req, "query failed", errors.New("boom"))
	if got := buf.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "a%0Ab") {
		t.Errorf("log = %q, want one line with the path as sent", got)
	}
}

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

func TestCloseRows_LogsBrokenRead(t *testing.T) {
	db, _, _, _ := newTestServer(t)
	buf := captureLog(t)

	rows, err := db.Query(`SELECT 10 / (3 - g) FROM generate_series(1, 5) g`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	closeRows("overlay: albums", rows)
	if n >= 5 {
		t.Fatalf("read all %d rows, the query was meant to break", n)
	}
	if got := buf.String(); !strings.HasPrefix(got, "overlay: albums: reading rows: ") || !strings.Contains(got, "division by zero") {
		t.Errorf("log = %q, want the broken read with its cause", got)
	}

	buf.Reset()
	rows, err = db.Query(`SELECT g FROM generate_series(1, 5) g`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for rows.Next() {
	}
	closeRows("overlay: albums", rows)
	if buf.Len() != 0 {
		t.Errorf("log = %q, want nothing after a complete read", buf.String())
	}
}
