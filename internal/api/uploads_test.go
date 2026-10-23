package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"kaikku/internal/lyrics"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 100, 100)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type releaseResp struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	RecordType  string `json:"record_type"`
	ReleaseDate string `json:"release_date"`
	CoverURL    string `json:"cover_url"`
	Draft       bool   `json:"draft"`
	Artists     []struct {
		ID, Name string
	} `json:"artists"`
	Tracks []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Explicit bool   `json:"explicit"`
		Artists  []struct {
			ID, Name string
		} `json:"artists"`
	} `json:"tracks"`
}

// newDraft — черновик через API и n треков в нём (как после загрузки файлов).
func newDraft(t *testing.T, db *sql.DB, router http.Handler, token string, n int) (string, []string) {
	t.Helper()
	var created struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/uploads/releases", token, "", http.StatusCreated), &created)
	if !isLocalID(created.ID) {
		t.Fatalf("release id %q", created.ID)
	}
	var tracks []string
	for i := range n {
		var id string
		if err := db.QueryRow(
			`INSERT INTO tracks (deezer_id, album_id, title, duration_ms, track_no, disc_no, storage_key, isrc, edited)
			 SELECT 'k' || nextval('kaikku_ids'), id, $2, 1000, $3, 1, 'uploads/' || md5(random()::text) || '.m4a', '', true
			   FROM albums WHERE deezer_id = $1 RETURNING deezer_id`,
			created.ID, "Song "+string(rune('A'+i)), i+1).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, id)
	}
	return created.ID, tracks
}

// Загружать может каждый; в «Моих загрузках» у каждого — свои релизы, у
// админа — все (и без режима админа), с именем автора у чужих.
func TestUploads_EveryoneUploadsAdminSeesAll(t *testing.T) {
	_, router, admin := newTestRouter(t)
	user := invitedUser(t, router, admin, "plain")
	var me struct {
		CanUpload bool `json:"can_upload"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/me", user, "", http.StatusOK), &me)
	if !me.CanUpload {
		t.Error("a plain user can't upload")
	}
	call(t, router, "POST", "/api/uploads/artists", user, `{"name":"X"}`, http.StatusCreated)
	var mine, theirs struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/uploads/releases", user, "", http.StatusCreated), &mine)
	json.Unmarshal(call(t, router, "POST", "/api/uploads/releases", admin, "", http.StatusCreated), &theirs)

	type list struct {
		Releases []struct{ ID, Owner string }
	}
	var l list
	json.Unmarshal(call(t, router, "GET", "/api/uploads", user, "", http.StatusOK), &l)
	if len(l.Releases) != 1 || l.Releases[0].ID != mine.ID || l.Releases[0].Owner != "" {
		t.Errorf("user's uploads = %+v", l.Releases)
	}
	json.Unmarshal(call(t, router, "GET", "/api/uploads", admin, "", http.StatusOK), &l)
	if len(l.Releases) != 2 || l.Releases[0].ID != theirs.ID || l.Releases[0].Owner != "" || l.Releases[1].Owner != "plain" {
		t.Errorf("admin's uploads = %+v", l.Releases)
	}
	var st struct{ Owner string }
	json.Unmarshal(call(t, router, "GET", "/api/uploads/releases/"+mine.ID, admin, "", http.StatusOK), &st)
	if st.Owner != "plain" {
		t.Errorf("editor owner = %q", st.Owner)
	}
	call(t, router, "GET", "/api/uploads/releases/"+theirs.ID, user, "", http.StatusNotFound)
}

func TestUploads_SaveAndPublish(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	listener := invitedUser(t, router, admin, "ears")
	id, tracks := newDraft(t, db, router, admin, 2)

	var artist struct{ ID, Name string }
	json.Unmarshal(call(t, router, "POST", "/api/uploads/artists", admin, `{"name":"  Nightvox  "}`, http.StatusCreated), &artist)
	if !isLocalID(artist.ID) || artist.Name != "Nightvox" {
		t.Fatalf("artist = %+v", artist)
	}

	// Черновик сохраняется и недозаполненным.
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin,
		`{"title":"","tracks":[{"id":"`+tracks[0]+`","title":""},{"id":"`+tracks[1]+`","title":"B"}]}`, http.StatusOK)

	body := `{"title":"Big Miss","artists":["` + artist.ID + `","1"],"record_type":"ep","release_date":"2026-10-15",
		"tracks":[{"id":"` + tracks[1] + `","title":"Second first","explicit":true,"artists":["` + artist.ID + `","2"]},
		          {"id":"` + tracks[0] + `","title":"Then this"}],"published":true}`
	var missing struct{ Missing []string }
	json.Unmarshal(call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body, http.StatusBadRequest), &missing)
	if !slices.Equal(missing.Missing, []string{"cover"}) {
		t.Errorf("missing = %v", missing.Missing)
	}
	call(t, router, "GET", "/api/discover/albums/"+id, listener, "", http.StatusNotFound) // всё ещё черновик

	rec := adminRequest(router, "PUT", "/api/uploads/releases/"+id+"/cover", admin, testJPEG(t), "image/jpeg")
	if rec.Code != http.StatusOK {
		t.Fatalf("cover: %d %s", rec.Code, rec.Body.String())
	}
	var st releaseResp
	json.Unmarshal(call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body, http.StatusOK), &st)
	if st.Draft || st.Title != "Big Miss" || st.RecordType != "ep" || st.ReleaseDate != "2026-10-15" || !strings.HasPrefix(st.CoverURL, "/media/covers/custom/") {
		t.Errorf("state = %+v", st)
	}
	if len(st.Artists) != 2 || st.Artists[0].Name != "Nightvox" || st.Artists[1].Name != "Test Artist" {
		t.Errorf("artists = %+v", st.Artists)
	}
	if len(st.Tracks) != 2 || st.Tracks[0].ID != tracks[1] || !st.Tracks[0].Explicit || len(st.Tracks[0].Artists) != 2 ||
		st.Tracks[1].Artists != nil {
		t.Errorf("tracks = %+v", st.Tracks)
	}

	var page localAlbumPage
	getJSON(t, router, "/api/discover/albums/"+id, listener, false, &page)
	if page.Album.Title != "Big Miss" || page.Tracks[0].Contributors[1].Name != "Other Artist" || page.Tracks[1].ArtistID != artist.ID {
		t.Errorf("published page = %+v", page)
	}
	var mine struct {
		Releases []struct {
			ID    string
			Draft bool
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/uploads", admin, "", http.StatusOK), &mine)
	if len(mine.Releases) != 1 || mine.Releases[0].ID != id || mine.Releases[0].Draft {
		t.Errorf("uploads = %+v", mine.Releases)
	}

	// Список треков должен совпадать с тем, что в релизе.
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, `{"title":"X","tracks":[{"id":"`+tracks[0]+`","title":"A"}]}`, http.StatusConflict)
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, `{"title":"X","artists":["k999999"],"tracks":[]}`, http.StatusBadRequest)

	// Снять с публикации.
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, strings.Replace(body, `"published":true`, `"published":false`, 1), http.StatusOK)
	call(t, router, "GET", "/api/discover/albums/"+id, listener, "", http.StatusNotFound)
}

// Релиз привязывается версией к другому и пропадает из дискографии,
// оставаясь в «Других версиях». Mock: релиз 9 — артиста 1.
func TestUploads_Version(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	listener := invitedUser(t, router, admin, "ears")
	id, tracks := newDraft(t, db, router, admin, 1)
	if rec := adminRequest(router, "PUT", "/api/uploads/releases/"+id+"/cover", admin, testJPEG(t), "image/jpeg"); rec.Code != http.StatusOK {
		t.Fatalf("cover: %d %s", rec.Code, rec.Body.String())
	}
	body := func(version string) string {
		return `{"title":"Deluxe","artists":["1"],"record_type":"album","release_date":"2026-10-20",
			"tracks":[{"id":"` + tracks[0] + `","title":"Bonus"}],"version_of":` + version + `,"published":true}`
	}
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(`"`+id+`"`), http.StatusBadRequest) // сам себе

	var st struct {
		VersionOf *struct{ ID, Title string } `json:"version_of"`
	}
	json.Unmarshal(call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(`"9"`), http.StatusOK), &st)
	if st.VersionOf == nil || st.VersionOf.ID != "9" || st.VersionOf.Title != "Test Album" {
		t.Errorf("version_of = %+v", st.VersionOf)
	}

	var page struct {
		Album struct {
			VersionOf string `json:"version_of"`
		}
		Versions []pageAlbum
	}
	getJSON(t, router, "/api/discover/albums/"+id, listener, false, &page)
	if page.Album.VersionOf != "9" || !slices.Equal(albumIDs(page.Versions), []string{"9"}) {
		t.Errorf("release page = %+v", page)
	}
	getJSON(t, router, "/api/discover/albums/9", listener, false, &page)
	if !slices.Equal(albumIDs(page.Versions), []string{id}) {
		t.Errorf("main release versions = %+v", page.Versions)
	}
	// Карточка версии — explicit, если explicit хоть один трек: свой или
	// добавленный с Deezer (mock: 888 — explicit).
	for _, c := range []struct {
		tracks string
		want   bool
	}{
		{`{"id":"` + tracks[0] + `","title":"Bonus"}`, false},
		{`{"id":"` + tracks[0] + `","title":"Bonus","explicit":true}`, true},
		{`{"id":"` + tracks[0] + `","title":"Bonus"},{"id":"888"}`, true},
	} {
		call(t, router, "PUT", "/api/uploads/releases/"+id, admin, `{"title":"Deluxe","artists":["1"],"record_type":"album",
			"release_date":"2026-10-20","tracks":[`+c.tracks+`],"version_of":"9","published":true}`, http.StatusOK)
		var p struct {
			Versions []struct{ Explicit bool }
		}
		getJSON(t, router, "/api/discover/albums/9", listener, false, &p)
		if len(p.Versions) != 1 || p.Versions[0].Explicit != c.want {
			t.Errorf("tracks %s: versions = %+v, want explicit %v", c.tracks, p.Versions, c.want)
		}
	}
	var disco struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/1/albums", listener, false, &disco)
	if slices.Contains(albumIDs(disco.Albums), id) {
		t.Errorf("a version must leave the discography: %v", albumIDs(disco.Albums))
	}

	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(`""`), http.StatusOK)
	var n int
	db.QueryRow(`SELECT count(*) FROM albums WHERE deezer_id = $1 AND version_of IS NULL`, id).Scan(&n)
	if n != 1 {
		t.Error("the version link must be cleared")
	}
}

// В релиз добавляются существующие треки (Deezer, другие свои) — под
// названием и обложкой релиза, на своём месте; сами они не меняются.
// Mock: трек 12345 — с релиза 9.
func TestUploads_OtherTracks(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	listener := invitedUser(t, router, admin, "ears")
	id, tracks := newDraft(t, db, router, admin, 1)
	if rec := adminRequest(router, "PUT", "/api/uploads/releases/"+id+"/cover", admin, testJPEG(t), "image/jpeg"); rec.Code != http.StatusOK {
		t.Fatalf("cover: %d %s", rec.Code, rec.Body.String())
	}
	body := func(trackList string) string {
		return `{"title":"Deluxe","artists":["1"],"record_type":"album","release_date":"2026-10-20",
			"tracks":[` + trackList + `],"published":true}`
	}
	own := `{"id":"` + tracks[0] + `","title":"Bonus"}`
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(own+`,`+own), http.StatusBadRequest)             // дважды
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(own+`,{"id":"k999999"}`), http.StatusBadRequest) // нет такого

	var st struct {
		Tracks []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Other bool   `json:"other"`
		} `json:"tracks"`
	}
	// У добавленного трека название не нужно: его здесь не правят.
	json.Unmarshal(call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(`{"id":"12345","title":"Renamed"},`+own), http.StatusOK), &st)
	if len(st.Tracks) != 2 || st.Tracks[0].ID != "12345" || !st.Tracks[0].Other || st.Tracks[0].Title != "Test Song" || st.Tracks[1].Other {
		t.Errorf("tracks = %+v", st.Tracks)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM tracks WHERE deezer_id = '12345' AND title = 'Renamed'`).Scan(&n)
	if n != 0 {
		t.Error("an added track must not be renamed by the release")
	}

	var page struct {
		Album struct {
			NbTracks int `json:"nb_tracks"`
		}
		Tracks []tracklistTrack
	}
	getJSON(t, router, "/api/discover/albums/"+id, listener, false, &page)
	if trackIDs(page.Tracks) != "12345 "+tracks[0]+" " || page.Tracks[0].Album != "Deluxe" || page.Tracks[0].TrackNo != 1 || page.Tracks[1].TrackNo != 2 {
		t.Errorf("release tracks = %+v", page.Tracks)
	}
	if page.Album.NbTracks != 2 {
		t.Errorf("nb_tracks = %d", page.Album.NbTracks)
	}
	// Карточка (медиатека, дискография, Subsonic) считает добавленный тоже: 1 с + 200 с.
	var mine struct {
		Releases []struct {
			NbTracks    int `json:"nb_tracks"`
			DurationSec int `json:"duration_sec"`
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/uploads", admin, "", http.StatusOK), &mine)
	if len(mine.Releases) != 1 || mine.Releases[0].NbTracks != 2 || mine.Releases[0].DurationSec != 201 {
		t.Errorf("card = %+v", mine.Releases)
	}

	// Без добавленных — обычный порядок своих треков.
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(own), http.StatusOK)
	db.QueryRow(`SELECT count(*) FROM albums WHERE deezer_id = $1 AND tracklist IS NULL`, id).Scan(&n)
	if n != 1 {
		t.Error("the tracklist must be cleared")
	}

	// Последний свой трек опубликованного релиза не удалить, но если в нём
	// есть добавленный — можно: свой заменили официальным.
	call(t, router, "DELETE", "/api/uploads/tracks/"+tracks[0], admin, "", http.StatusConflict)
	call(t, router, "PUT", "/api/uploads/releases/"+id, admin, body(`{"id":"12345"},`+own), http.StatusOK)
	call(t, router, "DELETE", "/api/uploads/tracks/"+tracks[0], admin, "", http.StatusNoContent)
	getJSON(t, router, "/api/discover/albums/"+id, listener, false, &page)
	if trackIDs(page.Tracks) != "12345 " {
		t.Errorf("after deleting the own track: %+v", page.Tracks)
	}
}

// Диски своего релиза: номер трека — место на своём диске, и у треков с
// других релизов тоже; новый файл ложится в конец последнего диска; удалённый
// трек уходит из треклиста вместе со своим диском. Mock: 12345 — с релиза 9.
func TestUploads_Discs(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	id, tracks := newDraft(t, db, router, admin, 3)
	tr := func(id string, disc int) string {
		return `{"id":"` + id + `","title":"T` + id + `","disc":` + strconv.Itoa(disc) + `}`
	}
	save := func(want int, list ...string) {
		t.Helper()
		call(t, router, "PUT", "/api/uploads/releases/"+id, admin, `{"title":"Deluxe","tracks":[`+strings.Join(list, ",")+`]}`, want)
	}
	page := func() string {
		t.Helper()
		var p struct{ Tracks []tracklistTrack }
		getJSON(t, router, "/api/discover/albums/"+id, admin, false, &p)
		out := ""
		for _, x := range p.Tracks {
			out += fmt.Sprintf("%s:%d.%d ", x.ID, x.DiscNo, x.TrackNo)
		}
		return out
	}
	a, b, c := tracks[0], tracks[1], tracks[2]

	save(http.StatusBadRequest, tr(a, 2), tr(b, 1), tr(c, 1)) // диск 2 раньше первого
	save(http.StatusBadRequest, tr(a, 1), tr(b, 1), tr(c, 100))
	save(http.StatusOK, tr(a, 1), tr(b, 2), tr(c, 2))
	if got, want := page(), a+":1.1 "+b+":2.1 "+c+":2.2 "; got != want {
		t.Errorf("own discs = %q, want %q", got, want)
	}

	// С добавленным треком диски хранятся и у него.
	save(http.StatusOK, `{"id":"12345","disc":1}`, tr(a, 1), tr(b, 2), tr(c, 2))
	if got, want := page(), "12345:1.1 "+a+":1.2 "+b+":2.1 "+c+":2.2 "; got != want {
		t.Errorf("discs with an added track = %q, want %q", got, want)
	}

	// Новый файл — в конец последнего диска.
	var lib string
	db.QueryRow(`SELECT id FROM albums WHERE deezer_id = $1`, id).Scan(&lib)
	added, disc, err := srv.insertOwnTrack(releaseRow{id: id, libraryID: lib}, "New", receivedAudio{key: "uploads/new.m4a", durationMs: 1000})
	if err != nil || disc != 2 {
		t.Fatalf("insert: disc %d, %v", disc, err)
	}
	if got, want := page(), "12345:1.1 "+a+":1.2 "+b+":2.1 "+c+":2.2 "+added+":2.3 "; got != want {
		t.Errorf("after an upload = %q, want %q", got, want)
	}

	// Удалённый трек уходит из треклиста, диски остальных — на местах.
	call(t, router, "DELETE", "/api/uploads/tracks/"+a, admin, "", http.StatusNoContent)
	if got, want := page(), "12345:1.1 "+b+":2.1 "+c+":2.2 "+added+":2.3 "; got != want {
		t.Errorf("after deleting a track = %q, want %q", got, want)
	}
}

// Релиз правят только автор и админ; чужой черновик не виден вовсе.
func TestUploads_Permissions(t *testing.T) {
	db, router, admin := newTestRouter(t)
	author := invitedUser(t, router, admin, "author5")
	stranger := invitedUser(t, router, admin, "stranger5")
	rel := seedOwnRelease(t, db, userIDOf(t, router, author), true)
	save := `{"title":"T","tracks":[{"id":"` + rel.tracks[0] + `","title":"a"},{"id":"` + rel.tracks[1] + `","title":"b"}]}`

	call(t, router, "GET", "/api/uploads/releases/"+rel.album, stranger, "", http.StatusNotFound)
	call(t, router, "PUT", "/api/uploads/releases/"+rel.album, stranger, save, http.StatusNotFound)
	call(t, router, "PUT", "/api/uploads/releases/"+rel.album, author, save, http.StatusOK)
	call(t, router, "PUT", "/api/uploads/releases/"+rel.album, admin, save, http.StatusOK)

	db.Exec(`UPDATE albums SET draft = false WHERE deezer_id = $1`, rel.album)
	call(t, router, "PUT", "/api/uploads/releases/"+rel.album, stranger, save, http.StatusForbidden)
	call(t, router, "DELETE", "/api/uploads/releases/"+rel.album, stranger, "", http.StatusForbidden)
	call(t, router, "PUT", "/api/uploads/releases/"+rel.album+"/cover", stranger, "", http.StatusForbidden)

	// Своего артиста правит тот, кто его завёл (и админ); артиста Deezer — только админ.
	var artistID, deezerArtistID string
	db.QueryRow(`SELECT id FROM artists WHERE deezer_id = $1`, rel.artist).Scan(&artistID)
	db.QueryRow(`SELECT id FROM artists WHERE deezer_id = '1'`).Scan(&deezerArtistID)
	call(t, router, "PUT", "/api/artists/"+artistID, stranger, `{"name":"Hacked"}`, http.StatusForbidden)
	call(t, router, "PUT", "/api/artists/"+artistID, author, `{"name":"Renamed"}`, http.StatusOK)
	call(t, router, "PUT", "/api/artists/"+deezerArtistID, author, `{"name":"Nope"}`, http.StatusForbidden)
	call(t, router, "PUT", "/api/artists/"+deezerArtistID, admin, `{"name":"Test Artist"}`, http.StatusOK)
	var page struct{ Artist struct{ Name string } }
	getJSON(t, router, "/api/discover/artists/"+rel.artist, stranger, false, &page)
	if page.Artist.Name != "Renamed" {
		t.Errorf("artist name = %q", page.Artist.Name)
	}
}

// Удаление убирает релиз отовсюду, а смена обложки обновляет снимки.
func TestUploads_CoverAndDelete(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	fan := invitedUser(t, router, admin, "fan6")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)
	db.Exec(`UPDATE albums SET cover_key = NULL, cover_custom = false WHERE deezer_id = $1`, rel.album)
	for _, id := range rel.tracks {
		os.MkdirAll(filepath.Join(srv.StorageDir, "uploads"), 0o755)
		os.WriteFile(filepath.Join(srv.StorageDir, "uploads", id+".m4a"), []byte("audio"), 0o644)
	}

	call(t, router, "PUT", "/api/me/liked/"+rel.tracks[0], fan, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/"+rel.album, fan, "", http.StatusNoContent)
	var p struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", fan, `{"title":"Mine"}`, http.StatusCreated), &p)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", fan, `{"deezer_ids":["`+rel.tracks[1]+`","12345"]}`, http.StatusOK)
	call(t, router, "POST", "/api/me/plays", fan, `{"deezer_id":"`+rel.tracks[0]+`"}`, http.StatusOK)
	db.Exec(`INSERT INTO lyrics (deezer_id, source, sync, body) VALUES ($1, 'upload', 'none', '{}')`, rel.tracks[0])

	// Обложка: снимок в лайках и сохранённом альбоме — с новой.
	for range 2 {
		rec := adminRequest(router, "PUT", "/api/uploads/releases/"+rel.album+"/cover", admin, testJPEG(t), "image/jpeg")
		if rec.Code != http.StatusOK {
			t.Fatalf("cover: %d %s", rec.Code, rec.Body.String())
		}
	}
	var cover string
	db.QueryRow(`SELECT cover_key FROM albums WHERE deezer_id = $1`, rel.album).Scan(&cover)
	for _, q := range []string{
		`SELECT meta->>'cover_url' FROM liked_tracks WHERE deezer_id = $1`,
		`SELECT meta->>'cover_url' FROM saved_albums WHERE deezer_id = $1`,
	} {
		var got string
		arg := rel.tracks[0]
		if strings.Contains(q, "saved_albums") {
			arg = rel.album
		}
		db.QueryRow(q, arg).Scan(&got)
		if got != "/media/"+cover {
			t.Errorf("%s: %q, want /media/%s", q, got, cover)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(srv.StorageDir, "covers", "custom", "*.jpg")); len(files) != 1 {
		t.Errorf("cover files: %v (the replaced one should be gone)", files)
	}

	call(t, router, "DELETE", "/api/uploads/releases/"+rel.album, admin, "", http.StatusNoContent)
	for _, q := range []string{
		`SELECT count(*) FROM liked_tracks`, `SELECT count(*) FROM saved_albums`, `SELECT count(*) FROM plays`,
		`SELECT count(*) FROM track_overrides WHERE deezer_id LIKE 'k%'`, `SELECT count(*) FROM tracks WHERE deezer_id LIKE 'k%'`,
		`SELECT count(*) FROM albums WHERE deezer_id LIKE 'k%'`,
	} {
		var n int
		db.QueryRow(q).Scan(&n)
		if n != 0 {
			t.Errorf("%s = %d after delete", q, n)
		}
	}
	var left int
	db.QueryRow(`SELECT count(*) FROM lyrics WHERE deezer_id = $1`, rel.tracks[0]).Scan(&left)
	if left != 0 {
		t.Error("lyrics left after delete")
	}
	db.QueryRow(`SELECT count(*) FROM playlist_items WHERE playlist_id = $1`, p.ID).Scan(&left)
	if left != 1 {
		t.Errorf("playlist items left = %d, want only the Deezer song", left)
	}
	if files, _ := filepath.Glob(filepath.Join(srv.StorageDir, "uploads", "*")); len(files) != 0 {
		t.Errorf("audio left: %v", files)
	}
	if files, _ := filepath.Glob(filepath.Join(srv.StorageDir, "covers", "custom", "*.jpg")); len(files) != 0 {
		t.Errorf("cover left: %v", files)
	}
	call(t, router, "GET", "/api/discover/albums/"+rel.album, fan, "", http.StatusNotFound)
}

func TestTitleFromFilename(t *testing.T) {
	for in, want := range map[string]string{
		"01 - Big Miss.flac":         "Big Miss",
		"07. Zombie.mp3":             "Zombie",
		"3_my_song.wav":              "my song",
		`C:\music\12 Track Name.m4a`: "Track Name",
		"2024.flac":                  "2024",
		"Song.v2.final.flac":         "Song.v2.final",
		".flac":                      "Track",
	} {
		if got := titleFromFilename(in); got != want {
			t.Errorf("titleFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func uploadAudio(router http.Handler, token, path string, body []byte) *httptest.ResponseRecorder {
	return adminRequest(router, http.MethodPost, path, token, body, "audio/wav")
}

func TestUploads_Audio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	db, router, admin, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	freeSpace = func(string) (uint64, error) { return 50 << 30, nil } // /tmp тестов может быть меньше гигабайта
	t.Cleanup(func() { freeSpace = statFree })
	fan := invitedUser(t, router, admin, "fan7")
	id, _ := newDraft(t, db, router, admin, 0)

	rec := uploadAudio(router, admin, "/api/uploads/releases/"+id+"/audio?name=02%20-%20Tone.wav", wavTone())
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var added struct {
		Track struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			DurationMs int    `json:"duration_ms"`
		}
	}
	json.Unmarshal(rec.Body.Bytes(), &added)
	if !isLocalID(added.Track.ID) || added.Track.Title != "Tone" || added.Track.DurationMs < 900 || added.Track.DurationMs > 1100 {
		t.Errorf("track = %+v", added.Track)
	}
	var key string
	db.QueryRow(`SELECT storage_key FROM tracks WHERE deezer_id = $1`, added.Track.ID).Scan(&key)
	if !strings.HasPrefix(key, "uploads/") {
		t.Errorf("storage key %q", key)
	}
	if _, err := os.Stat(filepath.Join(srv.StorageDir, key)); err != nil {
		t.Error(err)
	}
	if tmp, _ := filepath.Glob(filepath.Join(srv.StorageDir, ".upload-*")); len(tmp) != 0 {
		t.Errorf("temp files left: %v", tmp)
	}

	// Не аудио — отказ; файл с тегами и обложкой — название и обложка из него.
	rec = uploadAudio(router, admin, "/api/uploads/releases/"+id+"/audio?name=x.flac", []byte("#EXTM3U\n#EXTINF:1,\nhttp://example.com/a.flac\n"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("playlist upload: %d %s", rec.Code, rec.Body.String())
	}
	tagged := filepath.Join(t.TempDir(), "tagged.flac")
	pic := filepath.Join(t.TempDir(), "pic.png")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "color=c=blue:s=120x120:d=1", "-frames:v", "1", pic},
		{"-f", "lavfi", "-i", "sine=d=1", "-i", pic, "-map", "0:a", "-map", "1:v", "-c:v", "png", "-disposition:v", "attached_pic",
			"-metadata", "title=Tagged Song", "-metadata", "album=Tagged EP", "-metadata", "artist=Nightvox", tagged},
	} {
		if out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v %s", err, out)
		}
	}
	flac, _ := os.ReadFile(tagged)
	rec = uploadAudio(router, admin, "/api/uploads/releases/"+id+"/audio?name=whatever.flac", flac)
	var withTags struct {
		Track    struct{ ID, Title string }
		Tags     struct{ Album, Artist string }
		CoverURL string `json:"cover_url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &withTags)
	if rec.Code != http.StatusCreated || withTags.Track.Title != "Tagged Song" || withTags.Tags.Album != "Tagged EP" ||
		withTags.Tags.Artist != "Nightvox" || !strings.HasPrefix(withTags.CoverURL, "/media/covers/custom/") {
		t.Errorf("tagged upload: %d %s", rec.Code, rec.Body.String())
	}
	var st releaseResp
	getJSON(t, router, "/api/uploads/releases/"+id, admin, false, &st)
	if len(st.Tracks) != 2 || st.Tracks[1].ID != withTags.Track.ID || st.CoverURL != withTags.CoverURL {
		t.Errorf("release = %+v", st)
	}

	// Опубликованный релиз чужой человек не трогает; последний трек не удалить.
	db.Exec(`UPDATE albums SET draft = false WHERE deezer_id = $1`, id)
	if rec := uploadAudio(router, fan, "/api/uploads/releases/"+id+"/audio?name=a.wav", wavTone()); rec.Code != http.StatusForbidden {
		t.Errorf("stranger upload: %d", rec.Code)
	}
	call(t, router, "DELETE", "/api/uploads/tracks/"+added.Track.ID, fan, "", http.StatusForbidden)
	call(t, router, "DELETE", "/api/uploads/tracks/"+added.Track.ID, admin, "", http.StatusNoContent)
	call(t, router, "DELETE", "/api/uploads/tracks/"+withTags.Track.ID, admin, "", http.StatusConflict)
	if _, err := os.Stat(filepath.Join(srv.StorageDir, key)); !os.IsNotExist(err) {
		t.Error("deleted track's file is still there")
	}

	// Замена файла — тот же ID, новая длительность.
	call(t, router, "PUT", "/api/me/liked/"+withTags.Track.ID, fan, "", http.StatusNoContent)
	if rec := adminRequest(router, "PUT", "/api/uploads/tracks/"+withTags.Track.ID+"/audio", admin, wavTone(), "audio/wav"); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	var liked int
	db.QueryRow(`SELECT (meta->>'duration_ms')::int FROM liked_tracks WHERE deezer_id = $1`, withTags.Track.ID).Scan(&liked)
	if liked < 900 || liked > 1100 {
		t.Errorf("liked snapshot duration = %d", liked)
	}

	// Мало места — 507, ничего не сохраняется.
	freeSpace = func(string) (uint64, error) { return 100 << 20, nil }
	if rec := uploadAudio(router, admin, "/api/uploads/releases/"+id+"/audio?name=a.wav", wavTone()); rec.Code != http.StatusInsufficientStorage {
		t.Errorf("low disk: %d", rec.Code)
	}
}

// При старте API убираются брошенные загрузки, но не чужие свежие и не
// файлы треков.
func TestSweepUploads(t *testing.T) {
	db, _, _, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	rel := seedOwnRelease(t, db, "", false)
	up := filepath.Join(srv.StorageDir, "uploads")
	os.MkdirAll(up, 0o755)
	old := time.Now().Add(-2 * time.Hour)
	files := map[string]bool{ // путь → должен остаться
		filepath.Join(srv.StorageDir, ".upload-123"): false,
		filepath.Join(up, "x.m4a.part"):              false,
		filepath.Join(up, "orphan.m4a"):              false,
		filepath.Join(up, rel.tracks[0]+".m4a"):      true,
	}
	for p := range files {
		os.WriteFile(p, []byte("x"), 0o644)
		os.Chtimes(p, old, old)
	}
	fresh := filepath.Join(up, "fresh.m4a")
	os.WriteFile(fresh, []byte("x"), 0o644)
	files[fresh] = true

	srv.SweepUploads()
	for p, keep := range files {
		_, err := os.Stat(p)
		if keep != (err == nil) {
			t.Errorf("%s: kept=%v, want %v", filepath.Base(p), err == nil, keep)
		}
	}
}

func TestUploads_Lyrics(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.Lyrics = &lyrics.Service{DB: db, Providers: []lyrics.Provider{failProvider{t}}}
	fan := invitedUser(t, router, admin, "fan8")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)
	path := "/api/uploads/tracks/" + rel.tracks[0] + "/lyrics"

	call(t, router, "PUT", path, fan, `{"text":"nope"}`, http.StatusForbidden)
	call(t, router, "PUT", path, admin, `{"text":"[00:01.00]Hello\n[00:02.00]World"}`, http.StatusNoContent)
	var got struct {
		Lyrics *struct {
			Source, Sync string
			Lines        []struct{ T string }
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/lyrics/"+rel.tracks[0], fan, "", http.StatusOK), &got)
	if got.Lyrics == nil || got.Lyrics.Source != "upload" || got.Lyrics.Sync != "line" || len(got.Lyrics.Lines) != 2 {
		t.Errorf("lyrics = %+v", got.Lyrics)
	}
	var text struct{ Text string }
	json.Unmarshal(call(t, router, "GET", path, admin, "", http.StatusOK), &text)
	if text.Text != "[00:01.00]Hello\n[00:02.00]World" {
		t.Errorf("editor text = %q", text.Text)
	}
	call(t, router, "PUT", path, admin, `{"text":""}`, http.StatusNoContent)
	json.Unmarshal(call(t, router, "GET", "/api/lyrics/"+rel.tracks[0], fan, "", http.StatusOK), &got)
	if got.Lyrics != nil {
		t.Errorf("lyrics after clearing = %+v", got.Lyrics)
	}
}
