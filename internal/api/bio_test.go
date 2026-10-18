package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"kaikku/internal/deezer"
	"kaikku/internal/lastfm"
)

func TestBioText(t *testing.T) {
	cases := map[string]string{
		"<p>Schoolmates &amp; friends.</p><p>Second&nbsp;one.</p>": "Schoolmates & friends.\n\nSecond one.",
		"Line one<br/>line two\r\n\r\n\r\n\r\nNext":                "Line one\nline two\n\nNext",
		`«Буря» — группа. <a href="https://www.last.fm/music/%D0%91%D1%83%D1%80%D1%8F">Читать дальше на Last.fm</a>. Текст доступен на условиях CC-BY-SA.`: "«Буря» — группа.",
		` <a href="https://www.last.fm/music/X">Read more on Last.fm</a>`: "",
	}
	for in, want := range cases {
		if got := bioText(in); got != want {
			t.Errorf("bioText(%q) = %q, want %q", in, got, want)
		}
	}
	if !inLang("Буря — российская группа (Burya)", "ru") || inLang("Daft Punk — French duo", "ru") || !inLang("Daft Punk — дуэт", "en") {
		t.Error("inLang")
	}
}

type bioFakes struct {
	deezerBio map[string]string // lang → BIO у Deezer
	lastfmBio map[string]string // lang → content на Last.fm
	fail      atomic.Bool
	dzCalls   atomic.Int32
	lfCalls   atomic.Int32 // описания (artist.getInfo)
	tagCalls  atomic.Int32 // теги (artist.getTopTags)
	srv       *Server
}

func newBioTestServer(t *testing.T) (http.Handler, string, *bioFakes) {
	t.Helper()
	db, router, token, srv := newTestServer(t)
	db.Exec(`TRUNCATE artist_bios, artist_tags`)
	t.Cleanup(func() { db.Exec(`TRUNCATE artist_bios, artist_tags`) })
	f := &bioFakes{deezerBio: map[string]string{}, lastfmBio: map[string]string{}}
	dz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Query().Get("method") == "deezer.getUserData" {
			w.Write([]byte(`{"error":[],"results":{"checkForm":"tok"}}`))
			return
		}
		f.dzCalls.Add(1)
		var req struct{ Lang string }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		bio, _ := json.Marshal(f.deezerBio[req.Lang])
		fmt.Fprintf(w, `{"error":[],"results":{"BIO":{"BIO":%s}}}`, bio)
	}))
	t.Cleanup(dz.Close)
	lf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		q := r.URL.Query()
		if q.Get("artist") != "Test Artist" {
			w.Write([]byte(`{"error":6,"message":"not found"}`))
			return
		}
		if q.Get("method") == "artist.getTopTags" {
			f.tagCalls.Add(1)
			w.Write([]byte(`{"toptags":{"tag":[{"count":100,"name":"Hip Hop"},{"count":80,"name":"seen live"},{"count":60,"name":"Test Artist"},{"count":55,"name":"phonk"},{"count":3,"name":"jazz"}]}}`))
			return
		}
		f.lfCalls.Add(1)
		content, _ := json.Marshal(f.lastfmBio[q.Get("lang")])
		fmt.Fprintf(w, `{"artist":{"name":"Test Artist","url":"https://www.last.fm/music/Test+Artist","bio":{"content":%s}}}`, content)
	}))
	t.Cleanup(lf.Close)
	srv.DeezerSite = deezer.NewSiteWithBaseURL(dz.URL)
	srv.Lastfm = &lastfm.Client{Key: "k", Secret: "s", BaseURL: lf.URL}
	f.srv = srv
	return router, token, f
}

type bioReply struct {
	Text, Source, URL string
	Editable          bool
	Tags              []string
	TagsCustom        bool `json:"tags_custom"`
}

func getBio(t *testing.T, router http.Handler, token, id, lang string) bioReply {
	t.Helper()
	var b bioReply
	json.Unmarshal(call(t, router, "GET", "/api/discover/artists/"+id+"/bio?lang="+lang, token, "", http.StatusOK), &b)
	return b
}

func TestArtistBio_PicksLanguageAndCaches(t *testing.T) {
	router, token, f := newBioTestServer(t)
	f.deezerBio["en"] = "<p>A test artist.</p>"
	f.deezerBio["ru"] = "<p>A test artist.</p>" // у Deezer нет русского — отдаёт английский
	f.lastfmBio["ru"] = `Тестовый артист. <a href="https://www.last.fm/music/Test+Artist">Читать дальше</a>. CC-BY-SA.`

	if b := getBio(t, router, token, "1", "en"); b.Source != "deezer" || b.Text != "A test artist." || b.URL != "https://www.deezer.com/artist/1" {
		t.Fatalf("en: %+v", b)
	}
	if f.lfCalls.Load() != 0 {
		t.Fatal("Deezer had it in English — Last.fm is not asked")
	}
	if b := getBio(t, router, token, "1", "ru"); b.Source != "lastfm" || b.Text != "Тестовый артист." || b.URL != "https://www.last.fm/music/Test+Artist" {
		t.Fatalf("ru: %+v", b)
	}
	dz, lf := f.dzCalls.Load(), f.lfCalls.Load()
	getBio(t, router, token, "1", "ru")
	getBio(t, router, token, "1", "en")
	if f.dzCalls.Load() != dz || f.lfCalls.Load() != lf {
		t.Fatal("a found bio is kept, not fetched again")
	}

	// Ни у кого нет русского — показываем английский Deezer.
	f.lastfmBio["ru"] = ""
	if b := getBio(t, router, token, "2", "ru"); b.Source != "deezer" || b.Text != "A test artist." {
		t.Fatalf("fallback: %+v", b)
	}
}

func TestArtistBio_FailureIsNotRememberedAsEmpty(t *testing.T) {
	router, token, f := newBioTestServer(t)
	f.fail.Store(true)
	if b := getBio(t, router, token, "1", "en"); b.Text != "" {
		t.Fatalf("down: %+v", b)
	}
	f.fail.Store(false)
	f.deezerBio["en"] = "Back."
	if b := getBio(t, router, token, "1", "en"); b.Text != "Back." {
		t.Fatalf("after recovery: %+v", b)
	}
}

func TestArtistBio_CustomText(t *testing.T) {
	router, admin, f := newBioTestServer(t)
	f.deezerBio["en"] = "From Deezer."
	user := invitedUser(t, router, admin, "bioplain")

	// Артиста Deezer правит только админ.
	call(t, router, "PUT", "/api/discover/artists/1/bio", user, `{"text":"mine"}`, http.StatusForbidden)
	call(t, router, "PUT", "/api/discover/artists/1/bio", admin, `{"text":"  Own words.\r\n\r\n\r\nSecond.  "}`, http.StatusOK)
	for _, lang := range []string{"en", "ru"} {
		if b := getBio(t, router, user, "1", lang); b.Source != "custom" || b.Text != "Own words.\n\nSecond." || b.Editable {
			t.Fatalf("custom (%s): %+v", lang, b)
		}
	}
	// Пустой свой текст — описание скрыто.
	call(t, router, "PUT", "/api/discover/artists/1/bio", admin, `{"text":""}`, http.StatusOK)
	if b := getBio(t, router, user, "1", "en"); b.Text != "" || b.Source != "custom" {
		t.Fatalf("hidden: %+v", b)
	}
	call(t, router, "DELETE", "/api/discover/artists/1/bio", admin, "", http.StatusOK)
	if b := getBio(t, router, user, "1", "en"); b.Source != "deezer" {
		t.Fatalf("reset: %+v", b)
	}
	call(t, router, "PUT", "/api/discover/artists/1/bio", admin, fmt.Sprintf(`{"text":%q}`, strings.Repeat("я", bioMaxRunes+1)), http.StatusBadRequest)

	// Своего артиста — его создатель; чужой — нет. Внешние источники не спрашиваем.
	var own struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/uploads/artists", user, `{"name":"Mine"}`, http.StatusCreated), &own)
	calls := f.dzCalls.Load() + f.lfCalls.Load()
	if b := getBio(t, router, user, own.ID, "ru"); b.Text != "" || !b.Editable {
		t.Fatalf("own artist, no text: %+v", b)
	}
	if f.dzCalls.Load()+f.lfCalls.Load() != calls {
		t.Fatal("an own artist is not looked up on Deezer or Last.fm")
	}
	other := invitedUser(t, router, admin, "bioother")
	call(t, router, "PUT", "/api/discover/artists/"+own.ID+"/bio", other, `{"text":"x"}`, http.StatusForbidden)
	call(t, router, "PUT", "/api/discover/artists/"+own.ID+"/bio", user, `{"text":"Про меня."}`, http.StatusOK)
	if b := getBio(t, router, other, own.ID, "en"); b.Text != "Про меня." || b.Editable {
		t.Fatalf("own artist: %+v", b)
	}
	call(t, router, "PUT", "/api/discover/artists/k999999/bio", admin, `{"text":"x"}`, http.StatusNotFound)
}

// Теги приходят вместе с описанием: с Last.fm один раз, потом из базы; без
// мусора («seen live», имя самого артиста) и случайных (малый вес).
func TestArtistBio_Tags(t *testing.T) {
	router, token, f := newBioTestServer(t)
	b := getBio(t, router, token, "1", "en")
	if strings.Join(b.Tags, ",") != "hip-hop,phonk" {
		t.Fatalf("tags = %v", b.Tags)
	}
	getBio(t, router, token, "1", "ru")
	if f.tagCalls.Load() != 1 {
		t.Fatalf("tags fetched %d times", f.tagCalls.Load())
	}
	if b := getBio(t, router, token, "k1", "en"); b.Tags == nil || len(b.Tags) != 0 {
		t.Fatalf("own artist: %v", b.Tags)
	}
}

// Свои жанры: ставит админ (у своего артиста — владелец), Last.fm их не
// перезаписывает; сброс — снова с Last.fm.
func TestArtistTags_Custom(t *testing.T) {
	router, admin, f := newBioTestServer(t)
	user := invitedUser(t, router, admin, "tagsplain")
	getBio(t, router, admin, "1", "en") // с Last.fm: hip-hop, phonk

	call(t, router, "PUT", "/api/discover/artists/1/tags", user, `{"tags":["rock"]}`, http.StatusForbidden)
	call(t, router, "PUT", "/api/discover/artists/1/tags", admin, `{"tags":[" Hip Hop ","Drill","drill",""]}`, http.StatusOK)
	if b := getBio(t, router, user, "1", "en"); strings.Join(b.Tags, ",") != "hip-hop,drill" || !b.TagsCustom {
		t.Fatalf("custom tags: %+v", b)
	}
	// Фоновый проход и повторный запрос к Last.fm их не трогают.
	f.srv.fetchTags(context.Background(), "1", "Test Artist")
	if b := getBio(t, router, user, "1", "en"); strings.Join(b.Tags, ",") != "hip-hop,drill" {
		t.Fatalf("Last.fm overwrote custom tags: %v", b.Tags)
	}
	// Страница жанра видит артиста по своему жанру.
	call(t, router, "GET", "/api/genres/drill", user, "", http.StatusOK)

	call(t, router, "PUT", "/api/discover/artists/1/tags", admin, `{"tags":["a","b","c","d","e","f"]}`, http.StatusBadRequest)
	call(t, router, "PUT", "/api/discover/artists/1/tags", admin, fmt.Sprintf(`{"tags":[%q]}`, strings.Repeat("x", 41)), http.StatusBadRequest)

	before := f.tagCalls.Load()
	call(t, router, "DELETE", "/api/discover/artists/1/tags", admin, "", http.StatusOK)
	if b := getBio(t, router, user, "1", "en"); strings.Join(b.Tags, ",") != "hip-hop,phonk" || b.TagsCustom {
		t.Fatalf("reset: %+v", b)
	}
	if f.tagCalls.Load() != before+1 {
		t.Error("reset did not ask Last.fm again")
	}

	// Своего артиста — его создатель.
	var own struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/uploads/artists", user, `{"name":"Mine"}`, http.StatusCreated), &own)
	call(t, router, "PUT", "/api/discover/artists/"+own.ID+"/tags", user, `{"tags":["shoegaze"]}`, http.StatusOK)
	if b := getBio(t, router, user, own.ID, "en"); strings.Join(b.Tags, ",") != "shoegaze" {
		t.Fatalf("own artist: %+v", b)
	}
}
