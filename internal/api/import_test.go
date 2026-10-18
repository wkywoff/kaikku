package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kaikku/internal/deezer"
)

// Страница встраиваемого плеера Spotify — как она приходит (сокращённо).
const spotifyEmbedPage = `<!DOCTYPE html><html><head></head><body><div id="__next"></div>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"state":{"data":{"entity":{
"type":"playlist","name":"Road &amp; Trip","trackList":[
{"uri":"spotify:track:1","title":"Get Lucky (feat. Pharrell Williams and Nile Rodgers) - Radio Edit","subtitle":"Daft Punk, Pharrell Williams, Nile Rodgers","duration":248000},
{"uri":"spotify:track:2","title":"Выше","subtitle":"rokma","duration":150000},
{"uri":"spotify:track:3","title":"","subtitle":"Nobody","duration":1000}
]}}}}}}</script></body></html>`

func TestParseSpotifyEmbed(t *testing.T) {
	list, err := parseSpotifyEmbed([]byte(spotifyEmbedPage))
	if err != nil {
		t.Fatal(err)
	}
	if list.Title != "Road &amp; Trip" && list.Title != "Road & Trip" {
		t.Errorf("title = %q", list.Title)
	}
	if len(list.Tracks) != 2 || list.Truncated {
		t.Fatalf("tracks: %+v", list)
	}
	first := list.Tracks[0]
	if first.DurationMs != 248000 || len(first.Artists) != 3 || first.Artists[1] != "Pharrell Williams" {
		t.Errorf("first track: %+v", first)
	}
	if _, err := parseSpotifyEmbed([]byte("<html>nothing here</html>")); err == nil {
		t.Error("a page without data must be an error")
	}
}

func TestImportSpotifyLink(t *testing.T) {
	var asked string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte(spotifyEmbedPage))
	}))
	defer fake.Close()
	old := spotifyEmbedURL
	spotifyEmbedURL = fake.URL + "/embed/"
	defer func() { spotifyEmbedURL = old }()

	_, router, token := newTestRouter(t)
	body := call(t, router, "GET", "/api/import/spotify?link="+
		"https%3A%2F%2Fopen.spotify.com%2Fintl-de%2Fplaylist%2F37i9dQZF1DXcBWIGoYBM5M%3Fsi%3Dabc", token, "", http.StatusOK)
	if asked != "/embed/playlist/37i9dQZF1DXcBWIGoYBM5M" {
		t.Errorf("asked Spotify for %q", asked)
	}
	var list importList
	json.Unmarshal(body, &list)
	if len(list.Tracks) != 2 {
		t.Errorf("tracks: %+v", list)
	}
	call(t, router, "GET", "/api/import/spotify?link=https%3A%2F%2Fexample.com%2Fplaylist%2Fx", token, "", http.StatusBadRequest)
}

func TestBestImportMatch(t *testing.T) {
	tr := func(id, title, artist string, sec int) deezer.Track {
		return deezer.Track{ID: id, Title: title, Artist: artist, DurationMs: sec * 1000}
	}
	cases := []struct {
		name  string
		want  importTrack
		found []deezer.Track
		id    string // "" — ничего не подошло
	}{
		{"Spotify's dash note is Deezer's brackets",
			importTrack{Title: "Get Lucky - Radio Edit", Artists: []string{"Daft Punk", "Pharrell Williams"}, DurationMs: 248000},
			[]deezer.Track{tr("1", "Get Lucky (Radio Edit - feat. Pharrell Williams and Nile Rodgers)", "Daft Punk", 248)}, "1"},
		{"the original beats a live version",
			importTrack{Title: "Starboy", Artists: []string{"The Weeknd"}, DurationMs: 230000},
			[]deezer.Track{tr("live", "Starboy (Live)", "The Weeknd", 232), tr("orig", "Starboy", "The Weeknd", 230)}, "orig"},
		{"another artist's song with the same title is not it",
			importTrack{Title: "Hello", Artists: []string{"Adele"}, DurationMs: 295000},
			[]deezer.Track{tr("x", "Hello", "Lionel Richie", 251)}, ""},
		{"a much longer recording is not it",
			importTrack{Title: "Song", Artists: []string{"Band"}, DurationMs: 180000},
			[]deezer.Track{tr("x", "Song", "Band", 420)}, ""},
		{"diacritics and case don't matter",
			importTrack{Title: "Hoppípolla", Artists: []string{"Sigur Rós"}},
			[]deezer.Track{tr("s", "Hoppipolla", "Sigur Ros", 268)}, "s"},
		{"cyrillic",
			importTrack{Title: "Ёж", Artists: []string{"Буря"}, DurationMs: 200000},
			[]deezer.Track{tr("k", "Еж", "БУРЯ", 201)}, "k"},
		{"the same artist in another alphabet",
			importTrack{Title: "Причал", Artists: []string{"Tikhaya Burya"}, DurationMs: 204000},
			[]deezer.Track{tr("x", "Причал", "Nightvox", 174), tr("pm", "Причал", "Тихая Буря", 204)}, "pm"},
		{"a name with a comma, split by the source",
			importTrack{Title: "EARFQUAKE", Artists: []string{"Tyler", "The Creator"}, DurationMs: 190000},
			[]deezer.Track{tr("t", "EARFQUAKE", "Tyler, The Creator", 190)}, "t"},
	}
	for _, c := range cases {
		got := bestImportMatch(c.want, c.found)
		id := ""
		if got != nil {
			id = got.ID
		}
		if id != c.id {
			t.Errorf("%s: got %q, want %q", c.name, id, c.id)
		}
	}
}

func TestImportMatch(t *testing.T) {
	_, router, token := newTestRouter(t)
	body := call(t, router, "POST", "/api/import/match", token,
		`{"tracks":[{"title":"Test Song","artists":["Test Artist"],"duration_ms":201000},{"title":"Missing","artists":["Nobody"]}]}`,
		http.StatusOK)
	var got struct{ Matches []*deezer.Track }
	json.Unmarshal(body, &got)
	if len(got.Matches) != 2 || got.Matches[0] == nil || got.Matches[0].ID != "12345" || got.Matches[1] != nil {
		t.Fatalf("matches: %s", body)
	}

	tooMany := `{"tracks":[` + strings.TrimSuffix(strings.Repeat(`{"title":"x"},`, maxImportMatch+1), ",") + `]}`
	call(t, router, "POST", "/api/import/match", token, tooMany, http.StatusBadRequest)
}
