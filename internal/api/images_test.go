package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeezerImageProxy(t *testing.T) {
	const hash = "638e69b9caaf9f9f3f8826febea7b543"
	var gotPath string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-bytes"))
	}))
	t.Cleanup(cdn.Close)
	old := deezerImageHost
	deezerImageHost = cdn.URL + "/images/"
	t.Cleanup(func() { deezerImageHost = old })

	router := NewRouter(&Server{})
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil)) // без Bearer: так грузит <img>
		return rec
	}

	rec := get("/api/img/artist/" + hash + "/1000x1000-000000-80-0-0.jpg")
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if gotPath != "/images/artist/"+hash+"/1000x1000-000000-80-0-0.jpg" {
		t.Errorf("upstream path = %q", gotPath)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	// 200 — карточки на обычном (не «ретина») экране, srcset фронта.
	if rec := get("/api/img/cover/" + hash + "/200x200-000000-80-0-0.jpg"); rec.Code != http.StatusOK || gotPath != "/images/cover/"+hash+"/200x200-000000-80-0-0.jpg" {
		t.Errorf("200 px: status %d, upstream %q", rec.Code, gotPath)
	}

	// Только картинки Deezer, не произвольные пути.
	for _, bad := range []string{
		"/api/img/artist/" + hash + "/x.jpg",
		"/api/img/artist/not-a-hash/1000x1000-000000-80-0-0.jpg",
		"/api/img/secret/" + hash + "/1000x1000-000000-80-0-0.jpg",
		"/api/img/artist/" + hash + "/1000x1000-000000-80-0-0.php",
		"/api/img/artist/" + hash + "/9999x9999-000000-80-0-0.jpg", // размеры — только те, что нужны сайту
		"/api/img/artist/" + hash + "/1000x500-000000-80-0-0.jpg",
	} {
		gotPath = ""
		if rec := get(bad); rec.Code != http.StatusNotFound || gotPath != "" {
			t.Errorf("%s: status %d, upstream %q; want 404 without a request", bad, rec.Code, gotPath)
		}
	}
}
