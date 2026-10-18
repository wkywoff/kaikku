package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func claimID(t *testing.T, router http.Handler, path, token string) string {
	t.Helper()
	rec := adminRequest(router, "POST", path, token, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var resp struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID == "" {
		t.Fatalf("POST %s: no id in %s", path, rec.Body.String())
	}
	return resp.ID
}

// Админ правит артиста и альбом, у которых ничего не скачано: claim заводит
// строки по Deezer ID, повторный claim отдаёт те же, правка видна в Deezer-
// ответах, а на главной пустой альбом не появляется.
func TestClaimArtistAndAlbum(t *testing.T) {
	db, router, token := newTestRouter(t) // первый пользователь — админ

	artistID := claimID(t, router, "/api/artists/deezer/1/claim", token)
	if again := claimID(t, router, "/api/artists/deezer/1/claim", token); again != artistID {
		t.Errorf("second claim made another artist: %s vs %s", again, artistID)
	}
	albumID := claimID(t, router, "/api/albums/deezer/9/claim", token)
	var albumArtist, title string
	db.QueryRow(`SELECT artist_id, title FROM albums WHERE id = $1`, albumID).Scan(&albumArtist, &title)
	if albumArtist != artistID || title != "Test Album" {
		t.Errorf("album row: artist %s title %q", albumArtist, title)
	}

	if rec := adminRequest(router, "PUT", "/api/albums/"+albumID, token, []byte(`{"title":"Edited Album","record_type":"single"}`), "application/json"); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	rec := adminRequest(router, "GET", "/api/discover/albums/9", token, nil, "")
	var got struct {
		Album struct {
			Title     string `json:"title"`
			LibraryID string `json:"library_id"`
		} `json:"album"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Album.Title != "Edited Album" || got.Album.LibraryID != albumID {
		t.Errorf("overlay: %+v", got.Album)
	}

	// Не админ — нельзя.
	invite := newInvite(t, router, token)
	var reg struct{ Token string }
	json.Unmarshal(postJSON(router, "/api/register", `{"username":"plain-user","password":"testpass123","invite":"`+invite+`"}`, "").Body.Bytes(), &reg)
	user := reg.Token
	if rec := adminRequest(router, "POST", "/api/artists/deezer/1/claim", user, nil, ""); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin claim: %d, want 403", rec.Code)
	}
}

// Нескачанный трек правится по Deezer ID без скачивания: правка видна на
// странице альбома, сброс её убирает, а у скачанного правится его строка.
func TestEditTrackByDeezerID(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/tracks/deezer/501", token, `{"title":"Edited One","track_no":7}`, http.StatusOK)
	var jobs int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs`).Scan(&jobs)
	if jobs != 0 {
		t.Errorf("editing must not download the song, jobs: %d", jobs)
	}
	var got struct {
		Tracks []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			TrackNo int    `json:"track_no"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/discover/albums/77", token, "", http.StatusOK), &got)
	for _, tr := range got.Tracks {
		if tr.ID == "501" && (tr.Title != "Edited One" || tr.TrackNo != 7) {
			t.Errorf("overlay: %+v", tr)
		}
	}
	var edit struct {
		Title  string
		Edited bool
	}
	json.Unmarshal(call(t, router, "GET", "/api/tracks/deezer/501/edit", token, "", http.StatusOK), &edit)
	if edit.Title != "Edited One" || !edit.Edited {
		t.Errorf("edit form: %+v", edit)
	}
	json.Unmarshal(call(t, router, "POST", "/api/tracks/deezer/501/reset", token, "", http.StatusOK), &edit)
	if edit.Title != "Album Track One" || edit.Edited {
		t.Errorf("after reset: %+v", edit)
	}

	// Скачанный — правится его строка.
	var artistID, albumID, trackID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.QueryRow(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, 'Test Song', 200000, 't.m4a', '12345') RETURNING id`, albumID).Scan(&trackID)
	call(t, router, "PUT", "/api/tracks/deezer/12345", token, `{"title":"Renamed Song"}`, http.StatusOK)
	var title string
	var edited bool
	db.QueryRow(`SELECT title, edited FROM tracks WHERE id = $1`, trackID).Scan(&title, &edited)
	if title != "Renamed Song" || !edited {
		t.Errorf("library row: %q edited=%v", title, edited)
	}

	user := invitedUser(t, router, token, "track-edit-user")
	call(t, router, "PUT", "/api/tracks/deezer/501", user, `{"title":"x"}`, http.StatusForbidden)
}

// Окно правки показывает картинку, какую видят все: с Deezer (своих копий
// не храним) или свою, если её загрузили.
func TestEditImageURL(t *testing.T) {
	db, router, token := newTestRouter(t)
	albumID := claimID(t, router, "/api/albums/deezer/9/claim", token)
	artistID := claimID(t, router, "/api/artists/deezer/2/claim", token)
	image := func(path string) string {
		t.Helper()
		var got struct {
			ImageURL string `json:"image_url"`
		}
		json.Unmarshal(call(t, router, "GET", path, token, "", http.StatusOK), &got)
		return got.ImageURL
	}
	if got := image("/api/albums/" + albumID); got != "http://example.com/cover.jpg" {
		t.Errorf("album image %q, want Deezer's", got)
	}
	if got := image("/api/artists/" + artistID); got != "http://example.com/2.jpg" {
		t.Errorf("artist image %q, want Deezer's", got)
	}

	db.Exec(`UPDATE albums SET cover_key = 'covers/custom/ab.jpg', cover_custom = true WHERE id = $1`, albumID)
	db.Exec(`UPDATE artists SET picture_key = 'covers/custom/cd.jpg', picture_custom = true WHERE id = $1`, artistID)
	if got := image("/api/albums/" + albumID); got != "/media/covers/custom/ab.jpg" {
		t.Errorf("custom album image %q", got)
	}
	if got := image("/api/artists/" + artistID); got != "/media/covers/custom/cd.jpg" {
		t.Errorf("custom artist image %q", got)
	}
}
