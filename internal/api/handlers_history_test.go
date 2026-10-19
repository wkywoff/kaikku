package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestPlayHistoryAndRecent(t *testing.T) {
	db, router, token := newTestRouter(t)

	var pl struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", token, `{"title":"Mix"}`, http.StatusCreated), &pl)

	play := func(body string) { call(t, router, "POST", "/api/me/plays", token, body, http.StatusOK) }
	play(`{"deezer_id":"12345"}`)                                                  // без контекста — альбом трека (9)
	play(`{"deezer_id":"888","context":{"kind":"artist","id":"1"}}`)               // со страницы артиста
	play(`{"deezer_id":"501","context":{"kind":"playlist","id":"` + pl.ID + `"}}`) // из своего плейлиста
	play(`{"deezer_id":"12345","context":{"kind":"liked"}}`)
	play(`{"deezer_id":"12345","context":{"kind":"playlist","id":"00000000-0000-0000-0000-000000000000"}}`) // чужой/несуществующий — альбом
	call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"abc"}`, http.StatusBadRequest)

	var n int
	db.QueryRow(`SELECT count(*) FROM plays`).Scan(&n)
	if n != 5 {
		t.Fatalf("plays recorded: %d, want 5", n)
	}

	var recent struct {
		Items []struct {
			Kind     string
			Album    *struct{ ID, Title string }
			Artist   *struct{ ID, Name string }
			Playlist *struct{ ID, Title string }
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", token, "", http.StatusOK), &recent)
	// Последнее сверху, альбом 9 — один раз (два прослушивания).
	want := []string{"album", "liked", "playlist", "artist"}
	if len(recent.Items) != len(want) {
		t.Fatalf("recent: %+v", recent.Items)
	}
	for i, k := range want {
		if recent.Items[i].Kind != k {
			t.Fatalf("recent[%d] = %s, want %s (%+v)", i, recent.Items[i].Kind, k, recent.Items)
		}
	}
	if a := recent.Items[0].Album; a == nil || a.ID != "9" || a.Title != "Test Album" {
		t.Errorf("album item: %+v", a)
	}
	if a := recent.Items[3].Artist; a == nil || a.Name != "Test Artist" {
		t.Errorf("artist item: %+v", a)
	}
	if p := recent.Items[2].Playlist; p == nil || p.Title != "Mix" {
		t.Errorf("playlist item: %+v", p)
	}

	// Чужая история не видна.
	other := invitedUser(t, router, token, "history-other")
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", other, "", http.StatusOK), &recent)
	if len(recent.Items) != 0 {
		t.Errorf("someone else's history leaked: %+v", recent.Items)
	}
}

func TestFollowsSuggestionsAndReleases(t *testing.T) {
	_, router, token := newTestRouter(t)

	var played struct{ ID int64 }
	json.Unmarshal(call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"12345"}`, http.StatusOK), &played)
	if played.ID == 0 {
		t.Error("a recorded play must return its id (the player reports the listened time by it)")
	}
	call(t, router, "PUT", "/api/me/artists/1", token, "", http.StatusNoContent)

	var ids struct{ Artists []string }
	json.Unmarshal(call(t, router, "GET", "/api/me/collection", token, "", http.StatusOK), &ids)
	if len(ids.Artists) != 1 || ids.Artists[0] != "1" {
		t.Fatalf("followed ids: %+v", ids.Artists)
	}

	// Слушал артиста 1 → похожие на него (2, 3, 4), без него самого.
	var sug struct {
		Artists []struct{ ID string }
		BasedOn []string `json:"based_on"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/suggestions", token, "", http.StatusOK), &sug)
	if len(sug.Artists) != 3 || sug.Artists[0].ID != "2" || len(sug.BasedOn) != 1 || sug.BasedOn[0] != "Test Artist" {
		t.Errorf("suggestions: %+v", sug)
	}

	// Новые релизы: только свежий альбом, с именем артиста из подписки.
	var rel struct {
		Albums []struct {
			ID         string
			ArtistName string `json:"artist_name"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/releases", token, "", http.StatusOK), &rel)
	if len(rel.Albums) != 1 || rel.Albums[0].ID != "9" || rel.Albums[0].ArtistName != "Test Artist" {
		t.Errorf("releases: %+v", rel.Albums)
	}

	call(t, router, "DELETE", "/api/me/artists/1", token, "", http.StatusNoContent)
	json.Unmarshal(call(t, router, "GET", "/api/me/releases", token, "", http.StatusOK), &rel)
	if len(rel.Albums) != 0 {
		t.Errorf("releases after unfollow: %+v", rel.Albums)
	}
}

func TestDiscoverRelatedRadio(t *testing.T) {
	_, router, token := newTestRouter(t)

	var related struct{ Artists []struct{ ID string } }
	json.Unmarshal(call(t, router, "GET", "/api/discover/artists/1/related", token, "", http.StatusOK), &related)
	if len(related.Artists) != 3 {
		t.Errorf("related: %+v", related)
	}

	var radio struct{ Tracks []struct{ ID string } }
	json.Unmarshal(call(t, router, "GET", "/api/discover/artists/1/radio", token, "", http.StatusOK), &radio)
	if len(radio.Tracks) != 1 || radio.Tracks[0].ID != "501" {
		t.Errorf("radio: %+v", radio)
	}

	var track struct {
		Contributors []struct{ ID, Name string }
	}
	json.Unmarshal(call(t, router, "GET", "/api/discover/tracks/888", token, "", http.StatusOK), &track)
	if len(track.Contributors) != 2 || track.Contributors[1].Name != "Feat Artist" {
		t.Errorf("track contributors: %+v", track)
	}
}

// Страница артиста кешируется (Deezer спрашивается один раз), а правки
// админа накладываются при каждом ответе — и не попадают в кеш.
func TestArtistPageIsCachedButShowsEdits(t *testing.T) {
	_, router, token := newTestRouter(t)
	artistCalls.Store(0)

	type page struct {
		Artist struct{ Name string }
		Top    []struct {
			Contributors []struct{ Name string }
		}
	}
	get := func() page {
		var p page
		json.Unmarshal(call(t, router, "GET", "/api/discover/artists/1", token, "", http.StatusOK), &p)
		return p
	}
	if p := get(); p.Artist.Name != "Test Artist" || len(p.Top) != 1 {
		t.Fatalf("artist page: %+v", p)
	}
	get()
	if n := artistCalls.Load(); n != 1 {
		t.Errorf("Deezer asked %d times for the same artist, want 1", n)
	}

	var claimed struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/artists/deezer/1/claim", token, "", http.StatusOK), &claimed)
	call(t, router, "PUT", "/api/artists/"+claimed.ID, token, `{"name":"Renamed"}`, http.StatusOK)
	if p := get(); p.Artist.Name != "Renamed" || p.Top[0].Contributors[0].Name != "Renamed" {
		t.Errorf("the edit must show despite the cache: %+v", p)
	}
	call(t, router, "POST", "/api/artists/"+claimed.ID+"/reset", token, "", http.StatusOK)
	if p := get(); p.Artist.Name != "Test Artist" || p.Top[0].Contributors[0].Name != "Test Artist" {
		t.Errorf("after reset the cached Deezer data must be untouched by the edit: %+v", p)
	}
}

func TestLibraryTimes(t *testing.T) {
	db, router, token, _ := newTestServer(t)
	db.Exec(`TRUNCATE plays`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays`) })
	me := userIDOf(t, router, token)
	call(t, router, "PUT", "/api/me/albums/9", token, "", http.StatusNoContent)
	db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, played_at)
	         VALUES ($1, '12345', '{}', 'album', '9', now() - interval '1 day'), ($1, '12345', '{}', 'album', '9', now() - interval '3 days')`, me)
	var got struct {
		Added, Played map[string]time.Time
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/library/times", token, "", http.StatusOK), &got)
	if got.Added["album:9"].IsZero() {
		t.Fatalf("added: %v", got.Added)
	}
	if p := got.Played["album:9"]; time.Since(p) > 25*time.Hour || time.Since(p) < 23*time.Hour {
		t.Fatalf("played is the last play: %v", p)
	}
}
