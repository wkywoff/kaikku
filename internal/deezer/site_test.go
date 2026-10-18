package deezer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Гостевой токен берётся один раз; истёк (VALID_TOKEN_REQUIRED) — берётся
// новый и запрос повторяется. Без куки сессии страница артиста не отдаётся.
func TestSite_ArtistBio(t *testing.T) {
	tokens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("method") {
		case "deezer.getUserData":
			tokens++
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1", Path: "/"})
			fmt.Fprintf(w, `{"error":[],"results":{"checkForm":"tok%d"}}`, tokens)
		case "deezer.pageArtist":
			if c, err := r.Cookie("sid"); err != nil || c.Value != "s1" {
				w.Write([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"no session"},"results":{}}`))
				return
			}
			if q.Get("api_token") == "tok1" {
				w.Write([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"Invalid CSRF token"},"results":{}}`))
				return
			}
			body, _ := io.ReadAll(r.Body)
			switch {
			case strings.Contains(string(body), `"art_id":"27"`) && strings.Contains(string(body), `"lang":"ru"`):
				w.Write([]byte(`{"error":[],"results":{"DATA":{"ART_ID":"27"},"BIO":{"BIO":"<p>Дуэт.</p>","RESUME":""}}}`))
			case strings.Contains(string(body), `"art_id":"5"`):
				w.Write([]byte(`{"error":[],"results":{"DATA":{"ART_ID":"5"},"BIO":false}}`))
			default:
				w.Write([]byte(`{"error":{"DATA_ERROR":"artist not found"},"results":{}}`))
			}
		}
	}))
	defer srv.Close()
	s := NewSiteWithBaseURL(srv.URL)
	ctx := context.Background()

	bio, err := s.ArtistBio(ctx, "27", "ru")
	if err != nil || bio != "<p>Дуэт.</p>" || tokens != 2 {
		t.Fatalf("bio %q, err %v, tokens fetched %d", bio, err, tokens)
	}
	if bio, err := s.ArtistBio(ctx, "5", "ru"); bio != "" || err != nil {
		t.Fatalf("no bio: %q %v", bio, err)
	}
	if bio, err := s.ArtistBio(ctx, "999", "ru"); bio != "" || err != nil {
		t.Fatalf("no artist: %q %v", bio, err)
	}
	if tokens != 2 {
		t.Fatalf("the token must be reused, fetched %d times", tokens)
	}
}
