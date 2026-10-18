package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kaikku/internal/deezer"
)

func invitedUser(t *testing.T, router http.Handler, adminToken, name string) string {
	t.Helper()
	invite := newInvite(t, router, adminToken)
	rec := postJSON(router, "/api/register", `{"username":"`+name+`","password":"testpass123","invite":"`+invite+`"}`, "")
	var reg struct{ Token string }
	json.Unmarshal(rec.Body.Bytes(), &reg)
	if reg.Token == "" {
		t.Fatalf("register %s: %d %s", name, rec.Code, rec.Body.String())
	}
	return reg.Token
}

func call(t *testing.T, router http.Handler, method, path, token, body string, want int) []byte {
	t.Helper()
	rec := adminRequest(router, method, path, token, []byte(body), "application/json")
	if rec.Code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, rec.Code, rec.Body.String(), want)
	}
	return rec.Body.Bytes()
}

func TestLikesAndSavedAlbums(t *testing.T) {
	db, router, token := newTestRouter(t)

	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent) // повторно — не ошибка
	var liked struct {
		Tracks []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Album string `json:"album"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/liked", token, "", http.StatusOK), &liked)
	if len(liked.Tracks) != 1 || liked.Tracks[0].Title != "Test Song" || liked.Tracks[0].Album != "Test Album" {
		t.Fatalf("liked: %+v", liked.Tracks)
	}
	var queued int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).Scan(&queued)
	if queued != 1 {
		t.Errorf("a liked song should be queued for download, jobs: %d", queued)
	}

	call(t, router, "PUT", "/api/me/albums/77", token, "", http.StatusNoContent)
	var ids struct{ Liked, Albums []string }
	json.Unmarshal(call(t, router, "GET", "/api/me/collection", token, "", http.StatusOK), &ids)
	if len(ids.Liked) != 1 || ids.Liked[0] != "12345" || len(ids.Albums) != 1 || ids.Albums[0] != "77" {
		t.Errorf("collection ids: %+v", ids)
	}
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE source_ref IN ('deezer:501', 'deezer:502')`).Scan(&queued)
	if queued != 2 {
		t.Errorf("a saved album should be queued whole, jobs: %d", queued)
	}

	// У другого пользователя своя медиатека.
	other := invitedUser(t, router, token, "collection-other")
	json.Unmarshal(call(t, router, "GET", "/api/me/collection", other, "", http.StatusOK), &ids)
	if len(ids.Liked) != 0 || len(ids.Albums) != 0 {
		t.Errorf("someone else's collection leaked: %+v", ids)
	}

	call(t, router, "DELETE", "/api/me/liked/12345", token, "", http.StatusNoContent)
	call(t, router, "DELETE", "/api/me/albums/77", token, "", http.StatusNoContent)
	json.Unmarshal(call(t, router, "GET", "/api/me/collection", token, "", http.StatusOK), &ids)
	if len(ids.Liked) != 0 || len(ids.Albums) != 0 {
		t.Errorf("after removing: %+v", ids)
	}
}

func TestPlaylists(t *testing.T) {
	_, router, token := newTestRouter(t)
	other := invitedUser(t, router, token, "playlist-other")

	var p struct {
		ID         string `json:"id"`
		IsPublic   bool   `json:"is_public"`
		TrackCount int    `json:"track_count"`
		Mine       bool   `json:"mine"`
	}
	json.Unmarshal(call(t, router, "POST", "/api/playlists", token, `{"title":"Road trip"}`, http.StatusCreated), &p)
	if p.ID == "" || p.IsPublic || !p.Mine {
		t.Fatalf("created: %+v", p)
	}
	call(t, router, "POST", "/api/playlists", token, `{"title":"   "}`, http.StatusBadRequest)

	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", token, `{"deezer_ids":["12345","888"]}`, http.StatusOK)
	var got struct {
		Playlist struct {
			TrackCount int      `json:"track_count"`
			Covers     []string `json:"covers"`
		}
		Tracks []struct {
			ID     string `json:"id"`
			ItemID int64  `json:"item_id"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/playlists/"+p.ID, token, "", http.StatusOK), &got)
	if got.Playlist.TrackCount != 2 || len(got.Tracks) != 2 || got.Tracks[0].ID != "12345" || got.Tracks[1].ID != "888" {
		t.Fatalf("playlist: %+v", got)
	}

	// Приватный чужой — как будто его нет; добавлять в чужой нельзя.
	call(t, router, "GET", "/api/playlists/"+p.ID, other, "", http.StatusNotFound)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", other, `{"deezer_ids":["12345"]}`, http.StatusNotFound)
	call(t, router, "GET", "/api/playlists/not-a-uuid", token, "", http.StatusNotFound)

	// Публичный видят все, но меняет только владелец.
	call(t, router, "PUT", "/api/playlists/"+p.ID, token, `{"is_public":true,"description":"for the car"}`, http.StatusOK)
	var lists struct {
		Mine, Others []struct {
			ID    string
			Owner struct{ Username string }
		}
	}
	// В списке (главная, приложения) — только плейлисты друзей; по ссылке
	// публичный открывается и так.
	json.Unmarshal(call(t, router, "GET", "/api/playlists", other, "", http.StatusOK), &lists)
	if len(lists.Mine) != 0 || len(lists.Others) != 0 {
		t.Fatalf("not friends yet, other's list: %+v", lists)
	}
	call(t, router, "GET", "/api/playlists/"+p.ID, other, "", http.StatusOK)
	call(t, router, "PUT", "/api/users/"+userIDOf(t, router, token)+"/follow", other, "", http.StatusOK)
	json.Unmarshal(call(t, router, "GET", "/api/playlists", other, "", http.StatusOK), &lists)
	if len(lists.Others) != 0 {
		t.Fatalf("one-way follow, other's list: %+v", lists)
	}
	call(t, router, "PUT", "/api/users/"+userIDOf(t, router, other)+"/follow", token, "", http.StatusOK)
	json.Unmarshal(call(t, router, "GET", "/api/playlists", other, "", http.StatusOK), &lists)
	if len(lists.Mine) != 0 || len(lists.Others) != 1 || lists.Others[0].ID != p.ID {
		t.Fatalf("friends, other's list: %+v", lists)
	}
	call(t, router, "PUT", "/api/playlists/"+p.ID, other, `{"title":"mine now"}`, http.StatusNotFound)
	call(t, router, "DELETE", "/api/playlists/"+p.ID, other, "", http.StatusNotFound)

	call(t, router, "DELETE", "/api/playlists/"+p.ID+"/tracks/"+itoa(got.Tracks[0].ItemID), token, "", http.StatusNoContent)
	json.Unmarshal(call(t, router, "GET", "/api/playlists/"+p.ID, token, "", http.StatusOK), &got)
	if len(got.Tracks) != 1 || got.Tracks[0].ID != "888" {
		t.Errorf("after removing: %+v", got.Tracks)
	}

	call(t, router, "DELETE", "/api/playlists/"+p.ID, token, "", http.StatusNoContent)
	call(t, router, "GET", "/api/playlists/"+p.ID, token, "", http.StatusNotFound)
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// playlistOrder returns the Deezer IDs and item IDs of a playlist, in order.
func playlistOrder(t *testing.T, router http.Handler, token, id string) ([]string, []int64) {
	t.Helper()
	var got struct {
		Tracks []struct {
			ID     string `json:"id"`
			ItemID int64  `json:"item_id"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/playlists/"+id, token, "", http.StatusOK), &got)
	var ids []string
	var items []int64
	for _, tr := range got.Tracks {
		ids = append(ids, tr.ID)
		items = append(items, tr.ItemID)
	}
	return ids, items
}

func TestPlaylistReorder(t *testing.T) {
	db, router, token := newTestRouter(t)
	other := invitedUser(t, router, token, "reorder-other")
	var p struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", token, `{"title":"Order"}`, http.StatusCreated), &p)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", token, `{"deezer_ids":["12345","888","501"]}`, http.StatusOK)
	_, items := playlistOrder(t, router, token, p.ID)
	a, c := itoa(items[0]), itoa(items[2])
	move := func(item, before string, want int) {
		t.Helper()
		call(t, router, "PUT", "/api/playlists/"+p.ID+"/tracks/"+item+"/position", token, `{"before":`+before+`}`, want)
	}
	expect := func(want ...string) {
		t.Helper()
		if ids, _ := playlistOrder(t, router, token, p.ID); strings.Join(ids, ",") != strings.Join(want, ",") {
			t.Fatalf("order = %v, want %v", ids, want)
		}
	}

	move(c, a, http.StatusNoContent) // в начало
	expect("501", "12345", "888")
	move(c, "null", http.StatusNoContent) // в конец
	expect("12345", "888", "501")
	move(a, c, http.StatusNoContent) // в середину
	expect("888", "12345", "501")

	move(a, a, http.StatusBadRequest)
	move(a, "999999", http.StatusNotFound)
	call(t, router, "PUT", "/api/playlists/"+p.ID+"/tracks/"+a+"/position", other, `{"before":null}`, http.StatusNotFound)

	// Раз за разом в одну и ту же щель: позиции сходятся, и плейлист
	// перенумеровывается, а порядок остаётся верным.
	for i := 0; i < 40; i++ {
		move(c, a, http.StatusNoContent)
		move(a, c, http.StatusNoContent)
	}
	expect("888", "12345", "501")
	var maxPos float64
	db.QueryRow(`SELECT max(position) FROM playlist_items WHERE playlist_id = $1`, p.ID).Scan(&maxPos)
	if maxPos > 10 {
		t.Errorf("positions should have been renumbered, max = %v", maxPos)
	}
}

func TestPlaylistCover(t *testing.T) {
	db := openTestDB(t)
	deezerSrv := mockDeezerServer(t)
	t.Cleanup(deezerSrv.Close)
	srv := &Server{
		DB:         db,
		JWTSecret:  []byte("test-secret-at-least-32-bytes-long!!"),
		Deezer:     deezer.NewClientWithBaseURL(deezerSrv.URL),
		Background: func(f func()) { f() },
		StorageDir: t.TempDir(),
	}
	router := NewRouter(srv)
	token := registerTestUser(t, router)
	other := invitedUser(t, router, token, "cover-other")

	var p struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", token, `{"title":"Covered"}`, http.StatusCreated), &p)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", token, `{"deezer_ids":["12345"]}`, http.StatusOK)

	var img bytes.Buffer
	jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 100, 100)), nil)
	type info struct {
		Covers      []string `json:"covers"`
		Mosaic      []string `json:"mosaic"`
		CustomCover bool     `json:"custom_cover"`
	}
	upload := func(token string, want int) info {
		t.Helper()
		rec := adminRequest(router, "PUT", "/api/playlists/"+p.ID+"/cover", token, img.Bytes(), "image/jpeg")
		if rec.Code != want {
			t.Fatalf("upload: %d %s, want %d", rec.Code, rec.Body.String(), want)
		}
		var got info
		json.Unmarshal(rec.Body.Bytes(), &got)
		return got
	}
	file := func(url string) string { return filepath.Join(srv.StorageDir, strings.TrimPrefix(url, "/media/")) }

	first := upload(token, http.StatusOK)
	if !first.CustomCover || len(first.Covers) != 1 || !strings.HasPrefix(first.Covers[0], "/media/covers/custom/") ||
		len(first.Mosaic) != 1 || first.Mosaic[0] != "http://example.com/cover.jpg" {
		t.Fatalf("after upload: %+v", first)
	}
	if _, err := os.Stat(file(first.Covers[0])); err != nil {
		t.Fatalf("cover file: %v", err)
	}
	upload(other, http.StatusNotFound)

	second := upload(token, http.StatusOK)
	if _, err := os.Stat(file(first.Covers[0])); !os.IsNotExist(err) {
		t.Error("the replaced cover should be deleted")
	}

	var back info
	json.Unmarshal(call(t, router, "DELETE", "/api/playlists/"+p.ID+"/cover", token, "", http.StatusOK), &back)
	if back.CustomCover || len(back.Covers) != 1 || back.Covers[0] != "http://example.com/cover.jpg" {
		t.Errorf("after removing the cover: %+v", back)
	}
	if _, err := os.Stat(file(second.Covers[0])); !os.IsNotExist(err) {
		t.Error("the removed cover should be deleted")
	}

	third := upload(token, http.StatusOK)
	call(t, router, "DELETE", "/api/playlists/"+p.ID, token, "", http.StatusNoContent)
	if _, err := os.Stat(file(third.Covers[0])); !os.IsNotExist(err) {
		t.Error("a deleted playlist's cover should be deleted")
	}
}
