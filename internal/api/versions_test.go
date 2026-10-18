package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

type versionsPage struct {
	Album struct {
		ID        string `json:"id"`
		VersionOf string `json:"version_of"`
	}
	Versions []pageAlbum
}

// Версия релиза пропадает из дискографии и видна в «Других версиях» на
// странице основного и других версий; привязка к своей версии делает её
// основной. Mock: у артиста 1 релизы 9 и 10.
func TestAlbumVersions(t *testing.T) {
	_, router, token := newTestRouter(t)
	claim := func(dz string) string {
		var id string
		if err := json.Unmarshal(postJSON(router, "/api/albums/deezer/"+dz+"/claim", "", token).Body.Bytes(), &struct{ ID *string }{&id}); err != nil || id == "" {
			t.Fatalf("claim %s: %v", dz, err)
		}
		return id
	}
	single, album := claim("10"), claim("9")
	discography := func() []string {
		var d struct{ Albums []pageAlbum }
		getJSON(t, router, "/api/discover/artists/1/albums", token, true, &d)
		return albumIDs(d.Albums)
	}
	versions := func(dz string) versionsPage {
		var p versionsPage
		getJSON(t, router, "/api/discover/albums/"+dz, token, false, &p)
		return p
	}

	put(t, router, "/api/albums/"+single, token, `{"title":"Old Single","version_of":"10"}`, http.StatusBadRequest) // сам себе
	put(t, router, "/api/albums/"+single, token, `{"title":"Old Single","version_of":"9"}`, http.StatusOK)
	if got := discography(); !slices.Equal(got, []string{"9"}) {
		t.Errorf("discography with a version: %v", got)
	}
	if p := versions("9"); !slices.Equal(albumIDs(p.Versions), []string{"10"}) {
		t.Errorf("main release versions: %+v", p.Versions)
	}
	if p := versions("10"); p.Album.VersionOf != "9" || !slices.Equal(albumIDs(p.Versions), []string{"9"}) {
		t.Errorf("version page: %+v", p)
	}

	// Основной — версия своей версии: та становится основной.
	put(t, router, "/api/albums/"+album, token, `{"title":"Test Album","version_of":"10"}`, http.StatusOK)
	if got := discography(); !slices.Equal(got, []string{"10"}) {
		t.Errorf("after swapping the main release: %v", got)
	}

	put(t, router, "/api/albums/"+album, token, `{"title":"Test Album","version_of":""}`, http.StatusOK)
	if got := discography(); len(got) != 2 {
		t.Errorf("after unlinking: %v", got)
	}
}
