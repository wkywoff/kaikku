package lastfm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// Подпись по last.fm/api/authspec (п. 8): параметры по алфавиту
// «имязначение», затем секрет, md5; format не подписывается.
func TestSign_MatchesSpec(t *testing.T) {
	c := &Client{Key: "xxxxxxxx", Secret: "mysecret"}
	v := url.Values{"token": {"xxxxxxx"}, "method": {"auth.getSession"}, "api_key": {"xxxxxxxx"}, "format": {"json"}}
	sum := md5.Sum([]byte("api_keyxxxxxxxxmethodauth.getSessiontokenxxxxxxxmysecret"))
	if got := c.sign(v); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sign = %s", got)
	}
}

func fakeLastfm(t *testing.T, handle func(v url.Values) string) (*Client, *[]url.Values) {
	t.Helper()
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = append(got, r.Form)
		w.Write([]byte(handle(r.Form)))
	}))
	t.Cleanup(srv.Close)
	return &Client{Key: "k", Secret: "s", BaseURL: srv.URL}, &got
}

func TestScrobble_BatchParamsAndSignature(t *testing.T) {
	c, got := fakeLastfm(t, func(url.Values) string { return `{"scrobbles":{"@attr":{"accepted":2,"ignored":0}}}` })
	at := time.Unix(1700000000, 0)
	err := c.Scrobble(context.Background(), "sess", []Track{
		{Artist: "A", Title: "One", Album: "Al", Duration: 200, Started: at},
		{Artist: "B", Title: "Two", Started: at.Add(time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	v := (*got)[0]
	if v.Get("method") != "track.scrobble" || v.Get("sk") != "sess" || v.Get("artist[0]") != "A" || v.Get("track[1]") != "Two" ||
		v.Get("timestamp[1]") != "1700000060" || v.Get("duration[0]") != "200" || v.Has("album[1]") || v.Get("format") != "json" {
		t.Fatalf("params: %v", v)
	}
	sig := v.Get("api_sig")
	v.Del("api_sig")
	if c.sign(v) != sig {
		t.Fatal("bad signature")
	}
}

func TestErrors_Classified(t *testing.T) {
	code := 9
	c, _ := fakeLastfm(t, func(url.Values) string { return `{"error":` + strconv.Itoa(code) + `,"message":"x"}` })
	err := c.NowPlaying(context.Background(), "sess", Track{Artist: "A", Title: "T"})
	if !SessionInvalid(err) || Retryable(err) {
		t.Fatalf("code 9: %v", err)
	}
	code = 11
	err = c.NowPlaying(context.Background(), "sess", Track{Artist: "A", Title: "T"})
	if SessionInvalid(err) || !Retryable(err) {
		t.Fatalf("code 11: %v", err)
	}
	code = 6
	if err = c.NowPlaying(context.Background(), "sess", Track{Artist: "A", Title: "T"}); Retryable(err) {
		t.Fatalf("code 6 must not be retried: %v", err)
	}
	if !Retryable(errors.New("connection refused")) {
		t.Fatal("network errors are retried")
	}
}

func TestSession(t *testing.T) {
	c, got := fakeLastfm(t, func(url.Values) string { return `{"session":{"name":"testuser","key":"abc","subscriber":0}}` })
	name, key, err := c.Session(context.Background(), "tok")
	if err != nil || name != "testuser" || key != "abc" || (*got)[0].Get("token") != "tok" {
		t.Fatalf("%q %q %v", name, key, err)
	}
	if u := c.AuthURL("https://music.example/lastfm"); u != "https://www.last.fm/api/auth/?api_key=k&cb=https%3A%2F%2Fmusic.example%2Flastfm" {
		t.Fatal(u)
	}
}

func TestArtistBio(t *testing.T) {
	c, got := fakeLastfm(t, func(url.Values) string {
		return `{"artist":{"name":"Буря","url":"https://www.last.fm/music/%D0%91%D1%83%D1%80%D1%8F","bio":{"content":"«Буря» — рок-группа."}}}`
	})
	bio, link, err := c.ArtistBio(context.Background(), "Буря", "ru")
	if err != nil || bio != "«Буря» — рок-группа." || link != "https://www.last.fm/music/%D0%91%D1%83%D1%80%D1%8F" {
		t.Fatalf("%q %q %v", bio, link, err)
	}
	v := (*got)[0]
	if v.Get("method") != "artist.getInfo" || v.Get("artist") != "Буря" || v.Get("lang") != "ru" || v.Get("autocorrect") != "0" || v.Has("api_sig") {
		t.Fatalf("params: %v", v)
	}

	c, _ = fakeLastfm(t, func(url.Values) string { return `{"error":6,"message":"The artist you supplied could not be found"}` })
	if bio, _, err := c.ArtistBio(context.Background(), "nobody", "en"); bio != "" || err != nil {
		t.Fatalf("unknown artist: %q %v", bio, err)
	}
}

func TestArtistTopTags(t *testing.T) {
	c, got := fakeLastfm(t, func(v url.Values) string {
		if v.Get("artist") != "Burya" {
			return `{"error":6,"message":"The artist you supplied could not be found"}`
		}
		return `{"toptags":{"tag":[{"count":100,"name":"post-punk","url":"x"},{"count":42,"name":"russian rock"}],"@attr":{"artist":"Burya"}}}`
	})
	tags, err := c.ArtistTopTags(context.Background(), "Burya")
	if err != nil || len(tags) != 2 || tags[0] != (Tag{Name: "post-punk", Count: 100}) || tags[1].Count != 42 {
		t.Fatalf("tags = %v, %v", tags, err)
	}
	if v := (*got)[0]; v.Get("method") != "artist.getTopTags" || v.Get("autocorrect") != "0" || v.Has("api_sig") {
		t.Fatalf("params: %v", v)
	}
	if tags, err := c.ArtistTopTags(context.Background(), "Nobody"); err != nil || tags != nil {
		t.Fatalf("unknown artist: %v, %v", tags, err)
	}
}
