package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestParseLRC_RepeatsGapsAndMeta(t *testing.T) {
	l := parseLRC("[ar:Буря]\n[ti:Огни города]\n[00:10.50]Тихий вечер\n[00:14.00]\n[00:20.00][01:30.25]Огни города над рекой\n[01:35.000]Мой неспешный <00:01.00>шаг\n", 120, nil)
	if l == nil || l.Sync != SyncLine || len(l.Lines) != 4 {
		t.Fatalf("%+v", l)
	}
	a := l.Lines[0]
	if a.Text != "Тихий вечер" || !near(a.Start, 10.5) || !near(a.End, 14) {
		t.Errorf("gap must end the line: %+v", a)
	}
	if l.Lines[1].Text != "Огни города над рекой" || !near(l.Lines[2].Start, 90.25) || !near(l.Lines[2].End, 95) {
		t.Errorf("repeated chorus: %+v", l.Lines[1:3])
	}
	if l.Lines[3].Text != "Мой неспешный шаг" || !near(l.Lines[3].End, 120) {
		t.Errorf("word stamps / last end: %+v", l.Lines[3])
	}
	if parseLRC("Просто текст\nбез времени", 100, nil) != nil {
		t.Error("text without stamps is not LRC")
	}
	// Все отметки нулевые — это текст.
	if z := parseLRC("[00:00.00]Раз\n[00:00.00]Два", 100, nil); z == nil || z.Sync != SyncNone || len(z.Lines) != 2 {
		t.Errorf("zero stamps: %+v", z)
	}
}

func TestParseLRC_NeteaseCreditsDropped(t *testing.T) {
	l := parseLRC("[00:00.00] 作词 : Буря\n[00:01.00] 作曲 : Буря\n[00:12.30]Космос\n[00:15.00]纯音乐，请欣赏", 200, neteaseCredit)
	if l == nil || len(l.Lines) != 1 || l.Lines[0].Text != "Космос" {
		t.Fatalf("%+v", l)
	}
}

func TestPlainText(t *testing.T) {
	l := plainText("\n\nКуплет один\nвторая строка\n\n\n\nПрипев\n\n")
	if l == nil || l.Sync != SyncNone || len(l.Lines) != 4 || l.Lines[2].Text != "" || l.Lines[3].Text != "Припев" {
		t.Fatalf("%+v", l)
	}
	if plainText(" \n \n") != nil {
		t.Error("blank text")
	}
}

func TestSameSongAndArtist(t *testing.T) {
	tr := Track{Title: "Stronger (Remastered)", Duration: 312}
	if !sameSong(tr, "Stronger", 311.9) || sameSong(tr, "NOKIA", 241) || sameSong(tr, "Stronger", 250) {
		t.Error("sameSong")
	}
	if !sameSong(Track{Title: "НАД РЕКОЙ", Duration: 164}, "над рекой", 165) {
		t.Error("cyrillic case")
	}
	if !sameArtist("Nightvox", "NIGHTVOX") || !sameArtist("Kanye West", "Kanye West, Daft Punk") || sameArtist("Буря", "Причал") {
		t.Error("sameArtist")
	}
}

// fakeJWT — токен с exp через час (подпись не проверяется).
func fakeJWT() string {
	claims, _ := json.Marshal(map[string]int64{"exp": time.Now().Add(time.Hour).Unix()})
	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

func TestDeezer_WordsLinesTextAndMissing(t *testing.T) {
	answers := map[string]string{
		"1": `{"data":{"track":{"lyrics":{"writers":"A, B","text":"x","synchronizedLines":[{"line":"Work it","milliseconds":1000,"duration":900}],
			"synchronizedWordByWordLines":[{"start":1000,"end":1900,"words":[{"start":1000,"end":1400,"word":"Work"},{"start":1500,"end":1900,"word":"it"}]}]}}}}`,
		"2": `{"data":{"track":{"lyrics":{"text":"","synchronizedLines":[{"line":"Раз","milliseconds":5000,"duration":2000},{"line":"Два","milliseconds":9000,"duration":0}],"synchronizedWordByWordLines":[]}}}}`,
		"3": `{"data":{"track":{"lyrics":{"text":"Куплет\nстрока","synchronizedLines":[],"synchronizedWordByWordLines":[]}}}}`,
		"4": `{"data":{"track":{"lyrics":null}},"errors":[{"message":"Lyrics does not exists","type":"LyricsNotFoundError"}]}`,
	}
	auths := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			auths++
			fmt.Fprintf(w, `{"jwt":%q}`, fakeJWT())
			return
		}
		var req struct {
			Variables struct{ ID string } `json:"variables"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer e30.") {
			http.Error(w, "no token", 401)
			return
		}
		io.WriteString(w, answers[req.Variables.ID])
	}))
	defer srv.Close()
	oldA, oldP := deezerAuth, deezerPipe
	deezerAuth, deezerPipe = srv.URL+"/auth", srv.URL+"/pipe"
	defer func() { deezerAuth, deezerPipe = oldA, oldP }()

	d := &Deezer{}
	ctx := context.Background()
	l, err := d.Find(ctx, Track{DeezerID: "1"})
	if err != nil || l.Sync != SyncWord || l.Lines[0].Text != "Work it" || !near(l.Lines[0].Words[1].Start, 1.5) || l.Writers != "A, B" {
		t.Fatalf("words: %+v %v", l, err)
	}
	l, _ = d.Find(ctx, Track{DeezerID: "2", Duration: 20})
	if l.Sync != SyncLine || !near(l.Lines[0].End, 7) || !near(l.Lines[1].End, 20) {
		t.Errorf("lines: %+v", l.Lines)
	}
	l, _ = d.Find(ctx, Track{DeezerID: "3"})
	if l.Sync != SyncNone || len(l.Lines) != 2 {
		t.Errorf("text: %+v", l)
	}
	if l, err := d.Find(ctx, Track{DeezerID: "4"}); l != nil || err != nil {
		t.Errorf("missing: %+v %v", l, err)
	}
	if auths != 1 {
		t.Errorf("anonymous token fetched %d times, want 1 (cached)", auths)
	}
}

func TestNetEase_MatchesByTitleArtistAndLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/get"):
			io.WriteString(w, `{"result":{"songs":[
				{"id":1,"name":"Космос (Remix)","duration":180000,"artists":[{"name":"Буря"}]},
				{"id":2,"name":"Космос","duration":227400,"artists":[{"name":"Другой"}]},
				{"id":3,"name":"Космос","duration":226900,"artists":[{"name":"Буря"}]}]}}`)
		case r.URL.Query().Get("id") == "3":
			io.WriteString(w, `{"lrc":{"lyric":"[00:00.00] 作词 : Буря\n[00:12.00]Строка\n[00:15.50]Ещё"}}`)
		default:
			io.WriteString(w, `{"nolyric":true}`)
		}
	}))
	defer srv.Close()
	old := neteaseAPI
	neteaseAPI = srv.URL
	defer func() { neteaseAPI = old }()

	l, err := NetEase{}.Find(context.Background(), Track{Artist: "Буря", Title: "Космос", Duration: 227})
	if err != nil || l == nil || l.Sync != SyncLine || len(l.Lines) != 2 || l.Lines[0].Text != "Строка" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestLRCLIB_SyncedPlainMissing(t *testing.T) {
	busy := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("track_name") {
		case "busy": // перегружен — со второго раза отвечает
			if busy++; busy == 1 {
				http.Error(w, `{"name":"ServerOverloaded"}`, 503)
				return
			}
			io.WriteString(w, `{"plainLyrics":"Со второго раза"}`)
		case "synced":
			if r.URL.Query().Get("duration") != "200" || !strings.HasPrefix(r.Header.Get("User-Agent"), "kaikku") {
				http.Error(w, "bad", 400)
				return
			}
			io.WriteString(w, `{"syncedLyrics":"[00:01.00]Раз\n[00:03.00]Два","plainLyrics":"Раз\nДва"}`)
		case "plain":
			io.WriteString(w, `{"syncedLyrics":null,"plainLyrics":"Только текст"}`)
		case "instr":
			io.WriteString(w, `{"instrumental":true}`)
		default:
			http.Error(w, `{"code":404}`, 404)
		}
	}))
	defer srv.Close()
	old := lrclibAPI
	lrclibAPI = srv.URL
	defer func() { lrclibAPI = old }()

	ctx := context.Background()
	if l, err := (LRCLIB{}).Find(ctx, Track{Artist: "a", Title: "synced", Duration: 199.7}); err != nil || l == nil || l.Sync != SyncLine || len(l.Lines) != 2 {
		t.Errorf("synced: %+v %v", l, err)
	}
	if l, _ := (LRCLIB{}).Find(ctx, Track{Artist: "a", Title: "plain"}); l == nil || l.Sync != SyncNone {
		t.Errorf("plain: %+v", l)
	}
	oldRetry := retryAfter
	retryAfter = 10 * time.Millisecond
	defer func() { retryAfter = oldRetry }()
	if l, err := (LRCLIB{}).Find(ctx, Track{Artist: "a", Title: "busy"}); err != nil || l == nil || busy != 2 {
		t.Errorf("busy: %+v %v, requests %d", l, err, busy)
	}
	for _, title := range []string{"instr", "missing"} {
		if l, err := (LRCLIB{}).Find(ctx, Track{Artist: "a", Title: title}); l != nil || err != nil {
			t.Errorf("%s: %+v %v", title, l, err)
		}
	}
}

func TestFromText(t *testing.T) {
	synced := FromText("[ar:Me]\n[00:01.50]First line\n[00:04.00]Second\n", 10)
	if synced == nil || synced.Source != SourceUpload || synced.Sync != SyncLine || len(synced.Lines) != 2 ||
		synced.Lines[0].Start != 1.5 || synced.Lines[0].End != 4 {
		t.Fatalf("synced = %+v", synced)
	}
	if got := ToText(synced); got != "[00:01.50]First line\n[00:04.00]Second" {
		t.Errorf("ToText = %q", got)
	}
	plain := FromText("Verse one\n\n\nVerse two\n", 10)
	if plain == nil || plain.Sync != SyncNone || len(plain.Lines) != 3 || ToText(plain) != "Verse one\n\nVerse two" {
		t.Errorf("plain = %+v", plain)
	}
	if FromText("  \n ", 10) != nil {
		t.Error("blank text should be nil")
	}
}
