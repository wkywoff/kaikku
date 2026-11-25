package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"kaikku/internal/deezer"
)

type tracklistTrack struct {
	ID      string `json:"id"`
	Album   string `json:"album"`
	TrackNo int    `json:"track_no"`
	DiscNo  int    `json:"disc_no"`
}

func albumTracklist(t *testing.T, router http.Handler, token string) []tracklistTrack {
	t.Helper()
	var page struct{ Tracks []tracklistTrack }
	getJSON(t, router, "/api/discover/albums/77", token, false, &page)
	return page.Tracks
}

func trackIDs(tracks []tracklistTrack) string {
	s := ""
	for _, t := range tracks {
		s += t.ID + " "
	}
	return s
}

func TestAlbumTracklist_ReorderAndReplace(t *testing.T) {
	db, router, token := newTestRouter(t)
	var id string
	if err := json.Unmarshal(postJSON(router, "/api/albums/deezer/77/claim", "", token).Body.Bytes(), &struct{ ID *string }{&id}); err != nil || id == "" {
		t.Fatalf("claim: %v", err)
	}

	put(t, router, "/api/albums/"+id, token, `{"title":"Compilation","tracklist":["502","501"]}`, http.StatusOK)
	if got := trackIDs(albumTracklist(t, router, token)); got != "502 501 " {
		t.Fatalf("reordered: %s", got)
	}

	replace := func(body string, want int) {
		t.Helper()
		if rec := postJSON(router, "/api/albums/deezer/77/tracklist/replace", body, token); rec.Code != want {
			t.Fatalf("replace %s: %d %s, want %d", body, rec.Code, rec.Body.String(), want)
		}
	}
	replace(`{"track":"501","with":"12345"}`, http.StatusOK)
	tracks := albumTracklist(t, router, token)
	if trackIDs(tracks) != "502 12345 " {
		t.Fatalf("replaced: %s", trackIDs(tracks))
	}
	if got := tracks[1]; got.Album != "Compilation" || got.DiscNo != 2 || got.TrackNo != 2 {
		t.Errorf("track from another release: %+v", got)
	}
	replace(`{"track":"502","with":"404"}`, http.StatusBadRequest)
	replace(`{"track":"502","with":"12345"}`, http.StatusBadRequest)
	replace(`{"track":"501","with":"888"}`, http.StatusBadRequest)

	var edited struct{ Edited bool }
	getJSON(t, router, "/api/albums/"+id, token, false, &edited)
	if !edited.Edited {
		t.Error("an album with its own tracklist must be marked edited")
	}

	put(t, router, "/api/albums/"+id, token, `{"title":"Compilation","tracklist":["501","502"]}`, http.StatusOK)
	var n int
	db.QueryRow(`SELECT count(*) FROM albums WHERE id = $1 AND tracklist IS NULL`, id).Scan(&n)
	if n != 1 {
		t.Error("tracklist equal to Deezer's must clear the edit")
	}
	put(t, router, "/api/albums/"+id, token, `{"title":"Compilation","tracklist":["501","501"]}`, http.StatusBadRequest)

	put(t, router, "/api/albums/"+id, token, `{"title":"Compilation","tracklist":["502"]}`, http.StatusOK)
	if rec := postJSON(router, "/api/albums/"+id+"/reset", "", token); rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	if got := trackIDs(albumTracklist(t, router, token)); got != "501 502 " {
		t.Errorf("after reset: %s", got)
	}
}

func TestArrangeTracks_DeezerAddedTracksGoLast(t *testing.T) {
	_, _, _, srv := newTestServer(t)
	own := []deezer.Track{{ID: "501"}, {ID: "502"}, {ID: "503"}}
	got := srv.arrangeTracks(own, []string{"502", "501"}, []string{"501", "502"})
	ids := ""
	for _, t := range got {
		ids += t.ID + " "
	}
	if ids != "502 501 503 " {
		t.Errorf("got %s", ids)
	}
}

func TestTracklistPlayCountsForTheAlbum(t *testing.T) {
	db, router, token := newTestRouter(t)
	var id string
	if err := json.Unmarshal(postJSON(router, "/api/albums/deezer/77/claim", "", token).Body.Bytes(), &struct{ ID *string }{&id}); err != nil || id == "" {
		t.Fatalf("claim: %v", err)
	}
	put(t, router, "/api/albums/"+id, token, `{"title":"Compilation","tracklist":["12345","502"]}`, http.StatusOK)
	call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"12345","context":{"kind":"album","id":"77"}}`, http.StatusOK)

	var albumID, album string
	db.QueryRow(`SELECT meta->>'album_id', meta->>'album' FROM plays WHERE deezer_id = '12345'`).Scan(&albumID, &album)
	if albumID != "77" || album != "Compilation" {
		t.Errorf("play snapshot: album %s %q, want 77 Compilation", albumID, album)
	}

	db.Exec(`UPDATE plays SET meta = meta || '{"album_id":"9","album":"Test Album","cover_url":"http://example.com/cover.jpg"}'`)
	var recent struct {
		Items []struct {
			Album *struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				CoverURL string `json:"cover_url"`
			}
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", token, "", http.StatusOK), &recent)
	if len(recent.Items) != 1 || recent.Items[0].Album == nil || recent.Items[0].Album.Title != "Compilation" ||
		!strings.Contains(recent.Items[0].Album.CoverURL, "comp") {
		t.Errorf("recent: %+v", recent.Items)
	}
}
