package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPlayHistoryAndRecent(t *testing.T) {
	db, router, token := newTestRouter(t)

	var pl struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", token, `{"title":"Mix"}`, http.StatusCreated), &pl)

	play := func(body string) { call(t, router, "POST", "/api/me/plays", token, body, http.StatusOK) }
	play(`{"deezer_id":"12345"}`)
	play(`{"deezer_id":"888","context":{"kind":"artist","id":"1"}}`)
	play(`{"deezer_id":"501","context":{"kind":"playlist","id":"` + pl.ID + `"}}`)
	play(`{"deezer_id":"12345","context":{"kind":"liked"}}`)
	play(`{"deezer_id":"12345","context":{"kind":"playlist","id":"00000000-0000-0000-0000-000000000000"}}`)
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

	other := invitedUser(t, router, token, "history-other")
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", other, "", http.StatusOK), &recent)
	if len(recent.Items) != 0 {
		t.Errorf("someone else's history leaked: %+v", recent.Items)
	}
}

func TestRecentAlbumWithoutCoverInSnapshot(t *testing.T) {
	_, router, token := newTestRouter(t)
	call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"888"}`, http.StatusOK)

	var recent struct {
		Items []struct {
			Album *struct {
				ID       string
				CoverURL string `json:"cover_url"`
			}
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", token, "", http.StatusOK), &recent)
	if len(recent.Items) != 1 || recent.Items[0].Album == nil || recent.Items[0].Album.ID != "9" ||
		recent.Items[0].Album.CoverURL != "http://example.com/cover.jpg" {
		t.Errorf("recent: %+v", recent.Items)
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

	var sug struct {
		Artists []struct{ ID string }
		BasedOn []string `json:"based_on"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/suggestions", token, "", http.StatusOK), &sug)
	if len(sug.Artists) != 3 || sug.Artists[0].ID != "2" || len(sug.BasedOn) != 1 || sug.BasedOn[0] != "Test Artist" {
		t.Errorf("suggestions: %+v", sug)
	}

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

func TestNewReleases_LastMonth(t *testing.T) {
	_, router, token := newTestRouter(t)
	call(t, router, "PUT", "/api/me/artists/30", token, "", http.StatusNoContent)

	var rel struct{ Albums []pageAlbum }
	json.Unmarshal(call(t, router, "GET", "/api/me/releases", token, "", http.StatusOK), &rel)
	if got := albumIDs(rel.Albums); !slices.Contains(got, "41") || slices.Contains(got, "42") {
		t.Errorf("releases = %v, want 41 and not 42", got)
	}
}

func TestNewReleases_JointReleaseCreditsEveryone(t *testing.T) {
	db, router, token := newTestRouter(t)
	call(t, router, "PUT", "/api/me/artists/30", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/artists/31", token, "", http.StatusNoContent)
	db.Exec(`UPDATE followed_artists SET added_at = added_at - interval '1 day' WHERE deezer_id = '30'`)

	type album struct {
		ID           string
		ArtistID     string `json:"artist_id"`
		ArtistName   string `json:"artist_name"`
		Contributors []struct{ ID, Name string }
	}
	credits := func(a album) string {
		var names []string
		for _, c := range a.Contributors {
			names = append(names, c.ID+" "+c.Name)
		}
		return a.ArtistID + ": " + strings.Join(names, ", ")
	}
	for range 5 {
		var rel struct{ Albums []album }
		json.Unmarshal(call(t, router, "GET", "/api/me/releases", token, "", http.StatusOK), &rel)
		byID := map[string]album{}
		for _, a := range rel.Albums {
			byID[a.ID] = a
		}
		if len(byID) != len(rel.Albums) {
			t.Fatalf("a release is listed twice: %v", rel.Albums)
		}
		if got := credits(byID["40"]); got != "30: 30 Left Artist, 31 Right Artist" {
			t.Fatalf("joint release = %q", got)
		}
		if got := credits(byID["43"]); got != "31: 31 Right Artist, 30 Left Artist" {
			t.Fatalf("joint release Deezer won't give = %q", got)
		}
		if a := byID["41"]; a.ArtistName != "Left Artist" || len(a.Contributors) != 0 {
			t.Fatalf("one artist's release = %+v", a)
		}
	}

	newest := jsonList(t, newSubClient(t, router, token).get("getAlbumList2", url.Values{"type": {"newest"}}), "albumList2", "album")
	if i := slices.IndexFunc(newest, func(a map[string]any) bool { return a["id"] == "al40" }); i < 0 || newest[i]["displayArtist"] != "Left Artist, Right Artist" {
		t.Errorf("newest albums in apps = %v", newest)
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
