package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func adminRequest(router http.Handler, method, path, token string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// Правки альбома/трека/артиста в медиатеке видны на странице альбома,
// которая строится из ответа Deezer (mock: альбом 77, треки 501/502).
func TestEditsOverlayDeezerAlbum(t *testing.T) {
	db, router, token := newTestRouter(t) // первый пользователь — админ

	var artistID, albumID, trackID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Other Artist', '2') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id, record_type) VALUES ($1, 'Compilation', '77', 'compile') RETURNING id`, artistID).Scan(&albumID)
	db.QueryRow(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Album Track Two', 190000, 'x.m4a', '502') RETURNING id`, albumID, artistID).Scan(&trackID)

	edits := []struct{ path, body string }{
		{"/api/albums/" + albumID, `{"title":"Renamed Album","released_at":"2001-02-03","record_type":"ep"}`},
		{"/api/tracks/deezer/502", `{"title":"Renamed Track"}`},
		{"/api/artists/" + artistID, `{"name":"Renamed Artist"}`},
	}
	for _, e := range edits {
		if rec := adminRequest(router, "PUT", e.path, token, []byte(e.body), "application/json"); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", e.path, rec.Code, rec.Body.String())
		}
	}
	if rec := adminRequest(router, "PUT", "/api/albums/"+albumID, token, []byte(`{"title":"x","record_type":"mixtape"}`), "application/json"); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown record_type: %d, want 400", rec.Code)
	}

	rec := adminRequest(router, "GET", "/api/discover/albums/77", token, nil, "")
	var got struct {
		Album struct {
			Title       string `json:"title"`
			RecordType  string `json:"record_type"`
			ReleaseDate string `json:"release_date"`
			LibraryID   string `json:"library_id"`
		} `json:"album"`
		Tracks []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Artist string `json:"artist"`
			Album  string `json:"album"`
		} `json:"tracks"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Album.Title != "Renamed Album" || got.Album.RecordType != "ep" || got.Album.ReleaseDate != "2001-02-03" || got.Album.LibraryID != albumID {
		t.Errorf("album overlay: %+v", got.Album)
	}
	for _, tr := range got.Tracks {
		if tr.ID == "502" && (tr.Title != "Renamed Track" || tr.Artist != "Renamed Artist" || tr.Album != "Renamed Album") {
			t.Errorf("track overlay: %+v", tr)
		}
		if tr.ID == "501" && tr.Title != "Album Track One" {
			t.Errorf("unedited track changed: %+v", tr)
		}
	}
}

func TestUploadAlbumCover(t *testing.T) {
	db, router, token := newTestRouter(t)
	dir := t.TempDir()
	router = NewRouter(&Server{DB: db, JWTSecret: []byte("test-secret-at-least-32-bytes-long!!"), StorageDir: dir})

	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('A', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'B', '2') RETURNING id`, artistID).Scan(&albumID)

	if rec := adminRequest(router, "PUT", "/api/albums/"+albumID+"/cover", token, []byte("not an image"), "image/jpeg"); rec.Code != http.StatusBadRequest {
		t.Errorf("garbage upload: %d, want 400", rec.Code)
	}

	// «JPEG-бомба»: маленький файл, а в заголовке — огромная картинка.
	// Больше 2048 по стороне не берём: фронт шлёт до 1000, а уменьшение
	// (covers.go) распаковало бы её целиком.
	for _, side := range []uint16{5000, 2049} {
		var bomb bytes.Buffer
		jpeg.Encode(&bomb, image.NewRGBA(image.Rect(0, 0, 64, 64)), nil)
		b := bomb.Bytes()
		if i := bytes.Index(b, []byte{0xFF, 0xC0}); i >= 0 {
			binary.BigEndian.PutUint16(b[i+5:], side) // высота
			binary.BigEndian.PutUint16(b[i+7:], side) // ширина
		}
		if rec := adminRequest(router, "PUT", "/api/albums/"+albumID+"/cover", token, b, "image/jpeg"); rec.Code != http.StatusBadRequest {
			t.Errorf("%d×%d upload: %d, want 400", side, side, rec.Code)
		}
	}

	var buf bytes.Buffer
	jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 100, 100)), nil)
	rec := adminRequest(router, "PUT", "/api/albums/"+albumID+"/cover", token, buf.Bytes(), "image/jpeg")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var key string
	var custom bool
	db.QueryRow(`SELECT cover_key, cover_custom FROM albums WHERE id = $1`, albumID).Scan(&key, &custom)
	if !custom {
		t.Error("cover_custom not set")
	}
	if _, err := os.Stat(filepath.Join(dir, key)); err != nil {
		t.Errorf("uploaded cover not on disk: %v", err)
	}
}
