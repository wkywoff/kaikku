package ytdlp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

// Остальные тесты пакета ходят только в поддельный yt-dlp: YouTube Music
// включают лишь тесты ниже, на своём сервере.
func TestMain(m *testing.M) {
	musicSearchURL = ""
	os.Exit(m.Run())
}

// Фикстуры — настоящие ответы music.youtube.com (21.10.2026, с DE) без
// миниатюр, меню и служебных полей.
func musicFixture(t *testing.T, name string) []candidate {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := parseMusicSearch(f)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func findCandidate(t *testing.T, cs []candidate, id string) candidate {
	t.Helper()
	i := slices.IndexFunc(cs, func(c candidate) bool { return c.ID == id })
	if i < 0 {
		t.Fatalf("%s not among %d songs", id, len(cs))
	}
	return cs[i]
}

func boolPtr(b bool) *bool { return &b }

func TestParseMusicSearch_Fields(t *testing.T) {
	cs := musicFixture(t, "ytmusic_yeat_luh_geek.json")
	if len(cs) != 20 {
		t.Errorf("%d songs, want 20", len(cs))
	}
	got := cs[0]
	if got.ID != "-yb4WxgNqTA" || got.Title != "Luh gëek" || got.Album != "2 Alivë" ||
		got.DurationMs != 176000 || !got.Explicit || !got.Music || got.Channel != "Yeat" {
		t.Errorf("first song = %+v", got)
	}
	if c := findCandidate(t, musicFixture(t, "ytmusic_yeat_geek_high.json"), "gvxckeXOiXE"); c.Explicit || c.Title != "Gëek high (feat. Ken Carson)" {
		t.Errorf("clean Gëek high = %+v", c)
	}
}

func TestParseMusicSearch_SeveralArtists(t *testing.T) {
	cs := musicFixture(t, "ytmusic_daft_punk_get_lucky.json")
	got := cs[0]
	if !slices.Equal(got.Artists, []string{"Daft Punk", "Pharrell Williams", "Nile Rodgers"}) ||
		got.Album != "Random Access Memories" || got.DurationMs != 370000 {
		t.Errorf("first song = %+v", got)
	}
	// Исполнитель без ссылки на страницу артиста — всё равно исполнитель.
	if c := findCandidate(t, cs, cs[2].ID); !slices.Equal(c.Artists, []string{"Flash Mob Jazz"}) {
		t.Errorf("third song artists = %q", c.Artists)
	}
}

func TestParseMusicSearch_NoSongs(t *testing.T) {
	_, err := parseMusicSearch(strings.NewReader(`{"contents":{}}`))
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
	if _, err := parseMusicSearch(strings.NewReader(`<html>`)); err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want a parse error", err)
	}
}

func TestParseClock(t *testing.T) {
	for in, want := range map[string]int{"2:56": 176000, "1:02:03": 3723000, "0:30": 30000} {
		if got, ok := parseClock(in); !ok || got != want {
			t.Errorf("parseClock(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"7.6M plays", "2 Alivë", "", "1:x"} {
		if _, ok := parseClock(in); ok {
			t.Errorf("parseClock(%q) ok", in)
		}
	}
}

// Тот случай, из-за которого всё затевалось: explicit-трек Deezer.
func TestRankMusic_ExplicitLuhGeek(t *testing.T) {
	cs := musicFixture(t, "ytmusic_yeat_luh_geek.json")
	ranked, err := rankMusic(cs, Want{Artist: "Yeat", Title: "Luh gëek", Album: "2 Alivë (Geëk Pack)", DurationMs: 176000, Explicit: boolPtr(true)}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if ranked[0].ID != "-yb4WxgNqTA" {
		t.Errorf("best = %+v", ranked[0])
	}
}

func TestRankMusic_ExplicitDecidesBetweenVersions(t *testing.T) {
	cs := musicFixture(t, "ytmusic_yeat_geek_high.json")
	want := Want{Artist: "Yeat", OtherArtists: []string{"Ken Carson"}, Title: "Gëek high (feat. Ken Carson)", Album: "2 Alivë", DurationMs: 153000}
	for explicit, id := range map[bool]string{true: "L1Ck5QmzuUg", false: "gvxckeXOiXE"} {
		want.Explicit = boolPtr(explicit)
		ranked, err := rankMusic(cs, want, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if len(ranked) != 2 || ranked[0].ID != id {
			t.Errorf("explicit=%v: ranked %+v, want %s first of both versions", explicit, ranked, id)
		}
	}
}

func TestRankMusic_MismatchedExplicitIsStillAnAnswer(t *testing.T) {
	cs := []candidate{{ID: "e", Title: "Song", Channel: "Artist", DurationMs: 200000, Explicit: true, Music: true}}
	ranked, err := rankMusic(cs, Want{Artist: "Artist", Title: "Song", DurationMs: 200000, Explicit: boolPtr(false)}, 4000)
	if err != nil || ranked[0].ID != "e" {
		t.Errorf("ranked %+v, %v: labels don't always mark explicit, the only version must stay", ranked, err)
	}
}

func TestRankMusic_AlbumAndArtist(t *testing.T) {
	cs := musicFixture(t, "ytmusic_daft_punk_get_lucky.json")
	want := Want{Artist: "Daft Punk", OtherArtists: []string{"Pharrell Williams", "Nile Rodgers"},
		Title: "Get Lucky (feat. Pharrell Williams & Nile Rodgers)", Album: "Random Access Memories", DurationMs: 369000}
	ranked, err := rankMusic(cs, want, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if ranked[0].Album != "Random Access Memories" || ranked[0].DurationMs != 370000 {
		t.Errorf("best = %+v", ranked[0])
	}

	// «Get Lucky» Flash Mob Jazz из трибьюта — чужой исполнитель, даже когда
	// длина совпала бы.
	tribute := findCandidate(t, cs, cs[2].ID)
	want.DurationMs = tribute.DurationMs
	ranked, _ = rankMusic([]candidate{tribute}, want, 4000)
	if len(ranked) != 0 {
		t.Errorf("tribute by another artist accepted: %+v", ranked)
	}
}

func TestRankMusic_OtherVersionsAndDuration(t *testing.T) {
	cs := []candidate{
		{ID: "slow", Title: "Song (Slowed)", Channel: "Artist", DurationMs: 200000},
		{ID: "long", Title: "Song", Channel: "Artist", DurationMs: 260000},
	}
	if _, err := rankMusic(cs, Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 4000); !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

// musicServer serves body as the YouTube Music search answer and records
// the request bodies.
func musicServer(t *testing.T, status int, body string) *[]string {
	t.Helper()
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		requests = append(requests, string(b))
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := musicSearchURL
	musicSearchURL = srv.URL
	t.Cleanup(func() { musicSearchURL = old })
	return &requests
}

func TestFindMatches_YouTubeMusicFirst(t *testing.T) {
	fixture, err := os.ReadFile("testdata/ytmusic_yeat_luh_geek.json")
	if err != nil {
		t.Fatal(err)
	}
	requests := musicServer(t, http.StatusOK, string(fixture))
	calls := fakeYtDlp(t, searchLine("jdBxc61hGyA", "Luh gëek", 176))

	ids, err := FindMatches(context.Background(), Want{Artist: "Yeat", Title: "Luh gëek", DurationMs: 176000, Explicit: boolPtr(true)}, 15, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != "-yb4WxgNqTA" {
		t.Errorf("ids = %q", ids)
	}
	if len(*calls) != 0 {
		t.Errorf("yt-dlp started: %q", *calls)
	}
	if len(*requests) != 1 || !strings.Contains((*requests)[0], `"query":"Yeat Luh gëek"`) ||
		!strings.Contains((*requests)[0], `"clientName":"WEB_REMIX"`) {
		t.Errorf("requests = %q", *requests)
	}
}

func TestFindMatches_FallsBackToYouTubeSearch(t *testing.T) {
	for name, srv := range map[string]struct {
		status int
		body   string
	}{
		"broken":    {http.StatusBadRequest, `{"error":{"code":400}}`},
		"not found": {http.StatusOK, `{"contents":{}}`},
	} {
		t.Run(name, func(t *testing.T) {
			musicServer(t, srv.status, srv.body)
			calls := fakeYtDlp(t, searchLine("okA", "Song", 200))
			ids, err := FindMatches(context.Background(), Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 15, 4000)
			if err != nil || ids[0] != "okA" {
				t.Errorf("ids = %q, %v", ids, err)
			}
			if len(*calls) != 1 {
				t.Errorf("yt-dlp calls = %q, want the plain search", *calls)
			}
		})
	}
}
