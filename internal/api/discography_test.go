package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// Deezer уже отдаёт альбом 11, но в списке релизов артиста 1 его ещё нет:
// после того как страницу альбома открыли, он появляется в дискографии —
// первым, как самый свежий. Релиз совместный: появляется и у второго
// главного исполнителя (2), а у гостя (3) — нет.
func TestDiscography_OpenedReleaseMissingFromDeezerListIsAdded(t *testing.T) {
	db, router, token := newTestRouter(t)

	var albums struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/1/albums", token, false, &albums)
	if slices.Contains(albumIDs(albums.Albums), "11") {
		t.Fatalf("album 11 must not be in Deezer's list: %+v", albums.Albums)
	}

	var page struct{ Album pageAlbum }
	getJSON(t, router, "/api/discover/albums/11", token, false, &page)
	getJSON(t, router, "/api/discover/albums/9", token, false, &page) // есть в списке — запоминать нечего

	var extras int
	db.QueryRow(`SELECT count(*) FROM discography_extras`).Scan(&extras)
	if extras != 2 {
		t.Errorf("remembered %d rows, want album 11 for artists 1 and 2", extras)
	}
	var coMain, guest struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/2/albums", token, false, &coMain)
	getJSON(t, router, "/api/discover/artists/3/albums", token, false, &guest)
	if got := albumIDs(coMain.Albums); !slices.Equal(got, []string{"11"}) {
		t.Errorf("co-main artist discography = %v, want 11", got)
	}
	if len(guest.Albums) != 0 {
		t.Errorf("featured artist discography = %+v, want empty", guest.Albums)
	}
	getJSON(t, router, "/api/discover/artists/1/albums", token, false, &albums)
	if got := albumIDs(albums.Albums); !slices.Equal(got, []string{"11", "9", "10"}) {
		t.Errorf("discography = %v, want 11 9 10", got)
	}
	var artist struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/1", token, false, &artist)
	if got := albumIDs(artist.Albums); !slices.Contains(got, "11") {
		t.Errorf("artist page albums = %v, want 11 among them", got)
	}
}

// Запомненный релиз, который админ отдал другому артисту, у прежнего не
// показывается: правка исполнителей главнее.
func TestDiscography_RememberedReleaseFollowsCreditEdits(t *testing.T) {
	_, router, token := newTestRouter(t)

	var page struct{ Album pageAlbum }
	getJSON(t, router, "/api/discover/albums/11", token, false, &page)
	var id string
	if err := json.Unmarshal(postJSON(router, "/api/albums/deezer/11/claim", "", token).Body.Bytes(), &struct{ ID *string }{&id}); err != nil || id == "" {
		t.Fatalf("claim: %v", err)
	}
	put(t, router, "/api/albums/"+id, token, `{"title":"Late Album","artists":["2"]}`, http.StatusOK)

	var to, from struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/2/albums", token, false, &to)
	getJSON(t, router, "/api/discover/artists/1/albums", token, false, &from)
	if !slices.Contains(albumIDs(to.Albums), "11") {
		t.Errorf("artist 2 discography: %+v", to.Albums)
	}
	if slices.Contains(albumIDs(from.Albums), "11") {
		t.Errorf("artist 1 discography still has the moved release: %+v", from.Albums)
	}
}
