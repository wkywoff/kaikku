package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"kaikku/internal/deezer"
)

// Как Deezer раздал песни Dua Lipa по релизам при поиске (проверено живьём,
// 2026-10): обычный альбом, делюкс, сингл и чужой сборник вперемешку.
func TestSettleAlbums(t *testing.T) {
	song := func(id, album, isrc string) deezer.Track {
		return deezer.Track{ID: id, Title: id, ArtistID: "dua", AlbumID: album, ISRC: isrc}
	}
	release := func(kind, album string, size int, songs ...deezer.Track) importRelease {
		for i := len(songs); i < size; i++ {
			songs = append(songs, song(fmt.Sprintf("%s-filler-%d", album, i), album, fmt.Sprintf("%s-F%d", album, i)))
		}
		return importRelease{recordType: kind, tracks: songs}
	}
	releases := map[string]importRelease{
		"fn": release("album", "fn", 11,
			song("fn-lev", "fn", "LEV"), song("fn-dsn", "fn", "DSN"), song("fn-phy", "fn", "PHY"), song("fn-bmh", "fn", "BMH")),
		"moon": release("album", "moon", 17,
			song("moon-lev", "moon", "LEV"), song("moon-dsn", "moon", "DSN"), song("moon-phy", "moon", "PHY"), song("moon-bmh", "moon", "BMH"),
			song("moon-fever", "moon", "FEVER")),
		"pride": release("compile", "pride", 20, song("pride-phy", "pride", "PHY")),
		"bmh":   release("single", "bmh", 1, song("bmh-bmh", "bmh", "BMH")),
		"clean": release("album", "clean", 11, song("clean-lev", "clean", "LEV-CLEAN")),
	}
	ptr := func(t deezer.Track) *deezer.Track { return &t }
	ids := func(list []*deezer.Track) []string {
		var out []string
		for _, t := range list {
			if t == nil {
				out = append(out, "")
			} else {
				out = append(out, t.ID)
			}
		}
		return out
	}
	check := func(name string, got []*deezer.Track, want ...string) {
		t.Helper()
		if g := fmt.Sprint(ids(got)); g != fmt.Sprint(want) {
			t.Errorf("%s: got %s, want %v", name, g, want)
		}
	}

	matches := []*deezer.Track{
		ptr(song("fn-lev", "fn", "LEV")),
		ptr(song("moon-dsn", "moon", "DSN")),
		ptr(song("pride-phy", "pride", "PHY")),
		ptr(song("bmh-bmh", "bmh", "BMH")),
		nil,
		{ID: "other", ArtistID: "someone", AlbumID: "x", ISRC: "OTHER"},
	}
	check("the standard edition beats the deluxe, a single and a compilation",
		settleAlbums(matches, releases), "fn-lev", "fn-dsn", "fn-phy", "fn-bmh", "", "other")

	withBonus := append([]*deezer.Track{ptr(song("moon-fever", "moon", "FEVER"))}, matches...)
	check("a deluxe-only song pulls the rest to the deluxe",
		settleAlbums(withBonus, releases), "moon-fever", "moon-lev", "moon-dsn", "moon-phy", "moon-bmh", "", "other")

	clean := []*deezer.Track{ptr(song("clean-lev", "clean", "LEV-CLEAN")), ptr(song("moon-dsn", "moon", "DSN"))}
	check("a clean recording is not swapped for the explicit one",
		settleAlbums(clean, releases), "clean-lev", "moon-dsn")

	alone := []*deezer.Track{ptr(song("pride-phy", "pride", "PHY"))}
	check("only releases the playlist landed on are candidates",
		settleAlbums(alone, releases), "pride-phy")

	if got := albumsToSettle(matches); len(got) != 4 || got["x"] {
		t.Errorf("albums to settle: %v (an artist with one release has nothing to settle)", got)
	}
}

func TestImportAlbums(t *testing.T) {
	_, router, token := newTestRouter(t)
	body := call(t, router, "POST", "/api/import/albums", token,
		`{"tracks":[{"id":"12345","artist_id":"1","album_id":"9","isrc":"TEST00000001"},null,{"id":"12346","artist_id":"1","album_id":"10","isrc":"TEST00000002"}]}`,
		http.StatusOK)
	var got struct{ Matches []*deezer.Track }
	json.Unmarshal(body, &got)
	if len(got.Matches) != 3 || got.Matches[0] == nil || got.Matches[0].ID != "12345" || got.Matches[1] != nil || got.Matches[2].ID != "12346" {
		t.Fatalf("matches: %s", body)
	}
	call(t, router, "POST", "/api/import/albums", token, `{"tracks":[]}`, http.StatusBadRequest)
}
