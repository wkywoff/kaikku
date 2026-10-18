package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeTestJPEG(t *testing.T, path string, size int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{200, 40, 60, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSizedCover(t *testing.T) {
	storage := t.TempDir()
	const key = "covers/3007dedab68318ab6e68b09ca79f17f7.jpg"
	const small = "covers/artists/b224ceb79c858a8557b7ef1575c1df55.jpg"
	writeTestJPEG(t, filepath.Join(storage, key), 1000)
	writeTestJPEG(t, filepath.Join(storage, small), 50)
	srv := &Server{StorageDir: storage}
	router := NewRouter(srv)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	for i := 0; i < 2; i++ { // второй раз — из кеша
		rec := get("/api/cover/64/" + key)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		img, err := jpeg.Decode(rec.Body)
		if err != nil || img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
			t.Fatalf("pass %d: want a 64x64 JPEG, got %v %v", i, img.Bounds(), err)
		}
		if r, g, b, _ := img.At(32, 32).RGBA(); r>>8 < 180 || g>>8 > 70 || b>>8 > 90 {
			t.Errorf("colour changed: %d %d %d", r>>8, g>>8, b>>8)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Errorf("Cache-Control = %q", cc)
		}
	}
	cached := filepath.Join(storage, sizedDir, "64", key)
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("no cached copy: %v", err)
	}
	// 200 — карточки на обычном экране (srcset фронта).
	if rec := get("/api/cover/200/" + key); rec.Code != http.StatusOK {
		t.Errorf("200 px: status %d", rec.Code)
	} else if img, err := jpeg.Decode(rec.Body); err != nil || img.Bounds().Dx() != 200 {
		t.Errorf("200 px: want a 200×200 JPEG (%v)", err)
	}

	// Картинка меньше запрошенного — отдаётся как есть.
	if rec := get("/api/cover/300/" + small); rec.Code != http.StatusOK {
		t.Errorf("small original: status %d", rec.Code)
	} else if img, _ := jpeg.Decode(rec.Body); img == nil || img.Bounds().Dx() != 50 {
		t.Errorf("small original: want it unchanged")
	}

	for _, bad := range []string{
		"/api/cover/100/" + key,              // не наш размер
		"/api/cover/64/covers/../../etc.jpg", // выход из covers
		"/api/cover/64/tracks/3007dedab68318ab6e68b09ca79f17f7.jpg",
		"/api/cover/64/covers/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg", // нет файла
	} {
		// ".." роутер чистит сам (301 на другой путь) — главное, файла нет.
		if rec := get(bad); rec.Code == http.StatusOK {
			t.Errorf("%s: served a file", bad)
		}
	}

	// Удаление обложки удаляет и её копии.
	srv.removeStorageFiles(key)
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Errorf("cached copy left after removing the cover: %v", err)
	}
}
