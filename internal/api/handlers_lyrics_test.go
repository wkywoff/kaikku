package api

import (
	"net/http"
	"testing"
)

func TestLyrics_BadIDAndOff(t *testing.T) {
	_, router := newAuthTestServer(t)
	user := tokenOf(t, register(router, "owner", ""))
	if rec := request(router, "GET", "/api/lyrics/abc", user); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d", rec.Code)
	}
	if rec := request(router, "GET", "/api/lyrics/3135556", user); rec.Code != http.StatusOK || rec.Body.String() != "{\"lyrics\":null}\n" {
		t.Errorf("lyrics off = %d %q", rec.Code, rec.Body.String())
	}
	if rec := request(router, "GET", "/api/lyrics/3135556", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", rec.Code)
	}
}
