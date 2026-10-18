package api

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kaikku/internal/deezer"
)

// subClient — приложение с паролем для приложений.
type subClient struct {
	t      *testing.T
	router http.Handler
	user   string
	pass   string
}

func newSubClient(t *testing.T, router http.Handler, token string) *subClient {
	t.Helper()
	var a subsonicAccess
	json.Unmarshal(call(t, router, "POST", "/api/me/subsonic", token, "", http.StatusOK), &a)
	if !a.Enabled || len(a.Password) != 16 || a.Username == "" {
		t.Fatalf("app password: %+v", a)
	}
	return &subClient{t: t, router: router, user: a.Username, pass: a.Password}
}

// raw — ответ на method с токеном (t, s) и params.
func (c *subClient) raw(method string, params url.Values) *httptest.ResponseRecorder {
	if params == nil {
		params = url.Values{}
	}
	if params.Get("u") == "" {
		params.Set("u", c.user)
	}
	if params.Get("p") == "" && params.Get("t") == "" {
		sum := md5.Sum([]byte(c.pass + "salt42"))
		params.Set("t", hex.EncodeToString(sum[:]))
		params.Set("s", "salt42")
	}
	params.Set("v", "1.16.1")
	params.Set("c", "test")
	req := httptest.NewRequest(http.MethodGet, "/rest/"+method+".view?"+params.Encode(), nil)
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	return rec
}

// get — JSON-ответ; status должен быть ok.
func (c *subClient) get(method string, params url.Values) map[string]any {
	c.t.Helper()
	if params == nil {
		params = url.Values{}
	}
	params.Set("f", "json")
	resp := subResponse(c.t, c.raw(method, params))
	if resp["status"] != "ok" {
		c.t.Fatalf("%s: %v", method, resp["error"])
	}
	return resp
}

func subResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON (%d): %s", rec.Code, rec.Body.String())
	}
	return body["subsonic-response"]
}

// subErrorCode — код ошибки JSON-ответа (-1 — ошибки нет).
func subErrorCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	resp := subResponse(t, rec)
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return -1
	}
	return int(e["code"].(float64))
}

func TestSubsonicLogin(t *testing.T) {
	db, router, token := newTestRouter(t)

	// Без пароля для приложений не войти и основным паролем.
	var a subsonicAccess
	json.Unmarshal(call(t, router, "GET", "/api/me/subsonic", token, "", http.StatusOK), &a)
	if a.Enabled || a.Password != "" {
		t.Fatalf("enabled before it was created: %+v", a)
	}
	off := &subClient{t: t, router: router, user: a.Username, pass: "testpass123"}
	if code := subErrorCode(t, off.raw("ping", url.Values{"f": {"json"}})); code != subErrWrongCreds {
		t.Errorf("main password: code %d, want 40", code)
	}

	c := newSubClient(t, router, token)
	c.get("ping", nil) // токен с солью
	for _, p := range []string{c.pass, "enc:" + hex.EncodeToString([]byte(c.pass))} {
		if code := subErrorCode(t, c.raw("ping", url.Values{"f": {"json"}, "p": {p}})); code != -1 {
			t.Errorf("p=%s: code %d", p, code)
		}
	}
	if code := subErrorCode(t, c.raw("ping", url.Values{"f": {"json"}, "p": {"wrong"}})); code != subErrWrongCreds {
		t.Errorf("wrong password: code %d, want 40", code)
	}
	if code := subErrorCode(t, c.raw("ping", url.Values{"f": {"json"}, "apiKey": {"x"}})); code != subErrAuthNotAllow {
		t.Errorf("apiKey: code %d, want 42", code)
	}

	// Пароль хранится зашифрованным.
	var sealed []byte
	db.QueryRow(`SELECT subsonic_secret FROM users WHERE username = $1`, c.user).Scan(&sealed)
	if strings.Contains(string(sealed), c.pass) {
		t.Error("app password stored in the clear")
	}

	// Новый пароль — старый больше не подходит.
	old := c.pass
	c2 := newSubClient(t, router, token)
	if c2.pass == old {
		t.Fatal("same password twice")
	}
	if code := subErrorCode(t, c.raw("ping", url.Values{"f": {"json"}})); code != subErrWrongCreds {
		t.Errorf("old password: code %d, want 40", code)
	}
	c2.get("ping", nil)

	// Выключен — не войти.
	call(t, router, "DELETE", "/api/me/subsonic", token, "", http.StatusNoContent)
	if code := subErrorCode(t, c2.raw("ping", url.Values{"f": {"json"}})); code != subErrWrongCreds {
		t.Errorf("disabled: code %d, want 40", code)
	}
}

// После пяти неудач подряд — пауза, и верный пароль тоже не пускают.
func TestSubsonicLoginThrottled(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)
	for range loginFreeAttempts {
		c.raw("ping", url.Values{"f": {"json"}, "p": {"wrong"}})
	}
	if code := subErrorCode(t, c.raw("ping", url.Values{"f": {"json"}})); code != subErrWrongCreds {
		t.Errorf("right password during the pause: code %d, want 40", code)
	}
}

func TestSubsonicEnvelope(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)

	resp := c.get("getMusicFolders", nil)
	if resp["version"] != subsonicVersion || resp["openSubsonic"] != true || resp["type"] != "kaikku" || resp["serverVersion"] != Version {
		t.Errorf("envelope: %v", resp)
	}
	folders := resp["musicFolders"].(map[string]any)["musicFolder"].([]any)
	if len(folders) != 1 {
		t.Errorf("folders: %v", folders)
	}

	// XML — по умолчанию, те же данные атрибутами.
	rec := c.raw("getMusicFolders", nil)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("content type %q", ct)
	}
	var x struct {
		XMLName xml.Name `xml:"subsonic-response"`
		Status  string   `xml:"status,attr"`
		Folders []struct {
			ID   int    `xml:"id,attr"`
			Name string `xml:"name,attr"`
		} `xml:"musicFolders>musicFolder"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &x); err != nil || x.Status != "ok" || len(x.Folders) != 1 || x.Folders[0].Name != "kaikku" {
		t.Errorf("xml: %v %+v\n%s", err, x, rec.Body.String())
	}

	// Ошибка — status failed и код.
	if code := subErrorCode(t, c.raw("noSuchMethod", url.Values{"f": {"json"}})); code != subErrGeneric {
		t.Errorf("unknown method: code %d", code)
	}
	// Расширения OpenSubsonic — без входа.
	req := httptest.NewRequest(http.MethodGet, "/rest/getOpenSubsonicExtensions?f=json", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if resp := subResponse(t, rec); resp["status"] != "ok" {
		t.Errorf("extensions: %v", resp)
	}
	// getUser — только о себе.
	if user := c.get("getUser", url.Values{"username": {c.user}})["user"].(map[string]any); user["streamRole"] != true {
		t.Errorf("user: %v", user)
	}
	if code := subErrorCode(t, c.raw("getUser", url.Values{"f": {"json"}, "username": {"someone-else"}})); code != subErrForbidden {
		t.Errorf("other user: code %d", code)
	}
}

// jsonList — массив из JSON-ответа по пути ключей ("starred2", "song").
func jsonList(t *testing.T, v any, keys ...string) []map[string]any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	arr, _ := v.([]any)
	out := make([]map[string]any, len(arr))
	for i, x := range arr {
		out[i] = x.(map[string]any)
	}
	return out
}

func ids(items []map[string]any) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it["id"].(string))
	}
	return out
}

func TestSubsonicLibrary(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)

	c.get("star", url.Values{"id": {"t12345"}, "albumId": {"al9"}, "artistId": {"ar2"}})
	starred := c.get("getStarred2", nil)
	if got := ids(jsonList(t, starred, "starred2", "song")); !slices.Equal(got, []string{"t12345"}) {
		t.Errorf("starred songs %v", got)
	}
	if got := ids(jsonList(t, starred, "starred2", "album")); !slices.Equal(got, []string{"al9"}) {
		t.Errorf("starred albums %v", got)
	}
	if got := ids(jsonList(t, starred, "starred2", "artist")); !slices.Equal(got, []string{"ar2"}) {
		t.Errorf("starred artists %v (only the followed one)", got)
	}
	song := jsonList(t, starred, "starred2", "song")[0]
	if song["title"] != "Test Song" || song["albumId"] != "al9" || song["coverArt"] != "al9" || song["starred"] == nil || song["suffix"] != "m4a" {
		t.Errorf("song %v", song)
	}

	// Артисты медиатеки: подписка и исполнитель сохранённого.
	var names []string
	for _, idx := range jsonList(t, c.get("getArtists", nil), "artists", "index") {
		for _, a := range jsonList(t, idx, "artist") {
			names = append(names, idx["name"].(string)+":"+a["name"].(string))
		}
	}
	if !slices.Equal(names, []string{"O:Other Artist", "T:Test Artist"}) {
		t.Errorf("artists %v", names)
	}

	album := c.get("getAlbum", url.Values{"id": {"al9"}})["album"].(map[string]any)
	if album["name"] != "Test Album" || album["year"] != float64(2020) || album["starred"] == nil || album["songCount"] != float64(1) {
		t.Errorf("album %v", album)
	}
	if got := ids(jsonList(t, album, "song")); !slices.Equal(got, []string{"t12345"}) {
		t.Errorf("album songs %v", got)
	}
	if got := c.get("getSong", url.Values{"id": {"t12345"}})["song"].(map[string]any); got["artistId"] != "ar1" {
		t.Errorf("getSong %v", got)
	}
	artist := c.get("getArtist", url.Values{"id": {"ar1"}})["artist"].(map[string]any)
	if got := ids(jsonList(t, artist, "album")); !slices.Equal(got, []string{"al9", "al10"}) {
		t.Errorf("artist albums %v", got)
	}
	dir := c.get("getMusicDirectory", url.Values{"id": {"al9"}})["directory"].(map[string]any)
	if dir["parent"] != "ar1" || len(jsonList(t, dir, "child")) != 1 {
		t.Errorf("directory %v", dir)
	}
	if got := ids(jsonList(t, c.get("getAlbumList2", url.Values{"type": {"alphabeticalByName"}}), "albumList2", "album")); !slices.Equal(got, []string{"al9"}) {
		t.Errorf("album list %v", got)
	}

	// Пустой поиск — медиатека; с запросом — каталог.
	if got := ids(jsonList(t, c.get("search3", url.Values{"query": {`""`}}), "searchResult3", "song")); !slices.Equal(got, []string{"t12345"}) {
		t.Errorf("empty search %v", got)
	}
	if got := ids(jsonList(t, c.get("search3", url.Values{"query": {"test song"}}), "searchResult3", "song")); !slices.Contains(got, "t12345") {
		t.Errorf("search %v", got)
	}

	// Ошибки: не тот тип ID, нет параметра.
	if code := subErrorCode(t, c.raw("getAlbum", url.Values{"f": {"json"}, "id": {"t12345"}})); code != subErrNotFound {
		t.Errorf("song id as album: code %d", code)
	}
	if code := subErrorCode(t, c.raw("getAlbumList2", url.Values{"f": {"json"}})); code != subErrMissing {
		t.Errorf("no type: code %d", code)
	}

	// Скрытый админом релиз пропадает.
	call(t, router, "PUT", "/api/albums/deezer/9/hidden", token, `{"hidden":true}`, http.StatusOK)
	if code := subErrorCode(t, c.raw("getAlbum", url.Values{"f": {"json"}, "id": {"al9"}})); code != subErrNotFound {
		t.Errorf("hidden album: code %d", code)
	}
	call(t, router, "PUT", "/api/albums/deezer/9/hidden", token, `{"hidden":false}`, http.StatusOK)

	c.get("unstar", url.Values{"id": {"t12345"}, "albumId": {"al9"}})
	starred = c.get("getStarred2", nil)
	if len(jsonList(t, starred, "starred2", "song")) != 0 || len(jsonList(t, starred, "starred2", "album")) != 0 {
		t.Errorf("after unstar: %v", starred)
	}
}

// XML того же альбома: атрибуты и вложенные song.
func TestSubsonicAlbumXML(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)
	var x struct {
		Album struct {
			ID    string `xml:"id,attr"`
			Name  string `xml:"name,attr"`
			Songs []struct {
				ID       string `xml:"id,attr"`
				Duration int    `xml:"duration,attr"`
				IsDir    bool   `xml:"isDir,attr"`
			} `xml:"song"`
		} `xml:"album"`
	}
	rec := c.raw("getAlbum", url.Values{"id": {"al9"}})
	if err := xml.Unmarshal(rec.Body.Bytes(), &x); err != nil || x.Album.ID != "al9" || len(x.Album.Songs) != 1 || x.Album.Songs[0].Duration != 200 {
		t.Errorf("xml: %v %+v\n%s", err, x, rec.Body.String())
	}
}

func TestSubsonicStreamDownloaded(t *testing.T) {
	srv, _, token, _ := newStreamTestServer(t)
	db, router := srv.DB, NewRouter(srv)
	srv.StorageDir = t.TempDir()
	os.WriteFile(filepath.Join(srv.StorageDir, "a.m4a"), make([]byte, 1234), 0o644)
	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	if _, err := db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id, gain_db)
		VALUES ($1, $2, 'Test Song', 200000, 'a.m4a', '12345', -3)`, albumID, artistID); err != nil {
		t.Fatal(err)
	}
	c := newSubClient(t, router, token)

	rec := c.raw("stream", url.Values{"id": {"t12345"}})
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || !strings.HasPrefix(loc, "/media/a.m4a?st=") {
		t.Errorf("stream: %d %q %s", rec.Code, loc, rec.Body.String())
	}
	song := c.get("getSong", url.Values{"id": {"t12345"}})["song"].(map[string]any)
	if song["size"] != float64(1234) || song["replayGain"].(map[string]any)["trackGain"] != float64(-7) {
		t.Errorf("downloaded song: %v", song)
	}
	if code := subErrorCode(t, c.raw("stream", url.Values{"f": {"json"}, "id": {"al9"}})); code != subErrNotFound {
		t.Errorf("album id to stream: code %d", code)
	}
}

// Нескачанный — ссылка на поток API, а трек встаёт в очередь.
func TestSubsonicStreamNotDownloaded(t *testing.T) {
	srv, _, token, _ := newStreamTestServer(t)
	c := newSubClient(t, NewRouter(srv), token)
	rec := c.raw("stream", url.Values{"id": {"t12345"}})
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || !strings.HasPrefix(loc, "/api/stream/deezer/12345?st=") {
		t.Errorf("stream: %d %q %s", rec.Code, loc, rec.Body.String())
	}
	var status string
	srv.DB.QueryRow(`SELECT status FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).Scan(&status)
	if status == "" {
		t.Error("not queued")
	}
}

func TestSubsonicCoverArt(t *testing.T) {
	db, router, token, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	c := newSubClient(t, router, token)

	// Обложка Deezer (в моке — не CDN Deezer) — редиректом.
	rec := c.raw("getCoverArt", url.Values{"id": {"al9"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://example.com/cover.jpg" {
		t.Errorf("deezer cover: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Своя обложка — с диска, уменьшенная.
	key := "covers/custom/" + strings.Repeat("ab", 16) + ".jpg"
	os.MkdirAll(filepath.Join(srv.StorageDir, "covers", "custom"), 0o755)
	var buf bytes.Buffer
	jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 500, 500)), nil)
	os.WriteFile(filepath.Join(srv.StorageDir, key), buf.Bytes(), 0o644)
	var artistID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.Exec(`INSERT INTO albums (artist_id, title, deezer_id, cover_key, cover_custom) VALUES ($1, 'Test Album', '9', $2, true)`, artistID, key)
	for _, id := range []string{"al9", "t12345"} {
		rec = c.raw("getCoverArt", url.Values{"id": {id}, "size": {"64"}})
		img, err := jpeg.Decode(rec.Body)
		if rec.Code != http.StatusOK || err != nil || img.Bounds().Dx() != 64 {
			t.Errorf("%s custom cover: %d %v", id, rec.Code, err)
		}
	}
	rec = c.raw("getCoverArt", url.Values{"id": {"al9"}})
	if img, err := jpeg.Decode(rec.Body); err != nil || img.Bounds().Dx() != 500 {
		t.Errorf("full-size custom cover: %v", err)
	}
	if code := subErrorCode(t, c.raw("getCoverArt", url.Values{"f": {"json"}, "id": {"zz1"}})); code != subErrNotFound {
		t.Errorf("unknown id: code %d", code)
	}
}

func TestSubsonicDeezerImageAt(t *testing.T) {
	u := "https://cdn-images.dzcdn.net/images/cover/" + strings.Repeat("0f", 16) + "/1000x1000-000000-80-0-0.jpg"
	for size, want := range map[int]string{300: "300x300-000000-80-0-0.jpg", 0: "1000x1000-000000-80-0-0.jpg", 5000: "1000x1000-000000-80-0-0.jpg", 100: "200x200-000000-80-0-0.jpg"} {
		kind, hash, file, ok := deezerImageAt(u, size)
		if !ok || kind != "cover" || hash != strings.Repeat("0f", 16) || file != want {
			t.Errorf("size %d: %s %s %s %v", size, kind, hash, file, ok)
		}
	}
	if _, _, _, ok := deezerImageAt("/media/covers/x.jpg", 300); ok {
		t.Error("a local cover taken for Deezer's")
	}
}

func TestSubsonicScrobbleAndLyrics(t *testing.T) {
	db, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)
	plays := func() (n int) {
		db.QueryRow(`SELECT count(*) FROM plays WHERE deezer_id = '12345'`).Scan(&n)
		return
	}
	c.get("scrobble", url.Values{"id": {"t12345"}, "submission": {"false"}})
	if plays() != 0 {
		t.Error("now playing counted as a play")
	}
	c.get("scrobble", url.Values{"id": {"t12345"}, "time": {strconv.FormatInt(time.Now().UnixMilli(), 10)}})
	if plays() != 1 {
		t.Errorf("plays %d, want 1", plays())
	}
	// Текстов нет (Lyrics не настроен) — пустой список, не ошибка.
	if l := c.get("getLyricsBySongId", url.Values{"id": {"t12345"}})["lyricsList"]; l == nil {
		t.Error("no lyricsList")
	}
}

func TestSubsonicPlaylists(t *testing.T) {
	_, router, token := newTestRouter(t)
	owner := newSubClient(t, router, token)

	p := owner.get("createPlaylist", url.Values{"name": {"Road"}, "songId": {"t12345", "t888"}})["playlist"].(map[string]any)
	id := p["id"].(string)
	if !strings.HasPrefix(id, "pl") || p["name"] != "Road" || p["readonly"] != false {
		t.Fatalf("created %v", p)
	}
	if got := ids(jsonList(t, p, "entry")); !slices.Equal(got, []string{"t12345", "t888"}) {
		t.Errorf("entries %v", got)
	}
	owner.get("updatePlaylist", url.Values{"playlistId": {id}, "name": {"Road trip"}, "songIdToAdd": {"t501"}, "songIndexToRemove": {"0"}})
	p = owner.get("getPlaylist", url.Values{"id": {id}})["playlist"].(map[string]any)
	if got := ids(jsonList(t, p, "entry")); p["name"] != "Road trip" || !slices.Equal(got, []string{"t888", "t501"}) {
		t.Errorf("after update: %v %v", p["name"], got)
	}
	// createPlaylist с playlistId заменяет треки.
	owner.get("createPlaylist", url.Values{"playlistId": {id}, "songId": {"t12345"}})
	if got := ids(jsonList(t, owner.get("getPlaylist", url.Values{"id": {id}}), "playlist", "entry")); !slices.Equal(got, []string{"t12345"}) {
		t.Errorf("after replace: %v", got)
	}

	// Соавтор меняет треки, но не название и не удаляет.
	invite := newInvite(t, router, token)
	var reg struct{ Token string }
	json.Unmarshal(postJSON(router, "/api/register", `{"username":"subsonic-friend-`+strconv.FormatInt(time.Now().UnixNano(), 36)+`","password":"testpass123","invite":"`+invite+`"}`, "").Body.Bytes(), &reg)
	var me struct{ ID string }
	json.Unmarshal(call(t, router, "GET", "/api/me", reg.Token, "", http.StatusOK), &me)
	friend := newSubClient(t, router, reg.Token)
	if code := subErrorCode(t, friend.raw("getPlaylist", url.Values{"f": {"json"}, "id": {id}})); code != subErrNotFound {
		t.Errorf("private playlist seen by a stranger: code %d", code)
	}
	call(t, router, "PUT", "/api/users/"+me.ID+"/follow", token, "", http.StatusOK) // соавтор — только друг
	call(t, router, "PUT", "/api/users/"+userIDOf(t, router, token)+"/follow", reg.Token, "", http.StatusOK)
	call(t, router, "PUT", "/api/playlists/"+strings.TrimPrefix(id, "pl")+"/collaborators/"+me.ID, token, "", http.StatusNoContent)
	friend.get("updatePlaylist", url.Values{"playlistId": {id}, "songIdToAdd": {"t888"}})
	if code := subErrorCode(t, friend.raw("updatePlaylist", url.Values{"f": {"json"}, "playlistId": {id}, "name": {"Mine now"}})); code != subErrForbidden {
		t.Errorf("collaborator renames: code %d", code)
	}
	if code := subErrorCode(t, friend.raw("deletePlaylist", url.Values{"f": {"json"}, "id": {id}})); code != subErrForbidden {
		t.Errorf("collaborator deletes: code %d", code)
	}
	var found bool
	for _, pl := range jsonList(t, friend.get("getPlaylists", nil), "playlists", "playlist") {
		if pl["id"] == id && pl["readonly"] == false && pl["songCount"] == float64(2) {
			found = true
		}
	}
	if !found {
		t.Error("collaborator doesn't see the playlist as editable")
	}

	owner.get("deletePlaylist", url.Values{"id": {id}})
	if code := subErrorCode(t, owner.raw("getPlaylist", url.Values{"f": {"json"}, "id": {id}})); code != subErrNotFound {
		t.Errorf("deleted playlist: code %d", code)
	}
}

func TestSubsonicRadio(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)

	for _, q := range []struct{ method, id, key string }{
		{"getSimilarSongs2", "ar1", "similarSongs2"},
		{"getSimilarSongs", "t12345", "similarSongs"}, // радио исполнителя трека
	} {
		if got := ids(jsonList(t, c.get(q.method, url.Values{"id": {q.id}}), q.key, "song")); !slices.Equal(got, []string{"t501"}) {
			t.Errorf("%s %s: %v", q.method, q.id, got)
		}
	}
	if got := ids(jsonList(t, c.get("getTopSongs", url.Values{"artist": {"test artist"}}), "topSongs", "song")); !slices.Equal(got, []string{"t888"}) {
		t.Errorf("top songs %v", got)
	}
	c.get("star", url.Values{"id": {"t12345"}})
	if got := ids(jsonList(t, c.get("getRandomSongs", url.Values{"size": {"5"}}), "randomSongs", "song")); !slices.Equal(got, []string{"t12345"}) {
		t.Errorf("random songs %v", got)
	}
	info := c.get("getArtistInfo2", url.Values{"id": {"ar1"}})["artistInfo2"].(map[string]any)
	if got := ids(jsonList(t, info, "similarArtist")); !slices.Equal(got, []string{"ar2", "ar3", "ar4"}) {
		t.Errorf("similar artists %v", got)
	}
	if album := c.get("getAlbumInfo2", url.Values{"id": {"al9"}})["albumInfo"].(map[string]any); album["largeImageUrl"] != "http://example.com/cover.jpg" {
		t.Errorf("album info %v", album)
	}
	c.get("getGenres", nil)
	if code := subErrorCode(t, c.raw("getSongsByGenre", url.Values{"f": {"json"}, "genre": {"no such genre"}})); code != subErrNotFound {
		t.Errorf("unknown genre: code %d", code)
	}
}

func TestSubsonicCoverIDs(t *testing.T) {
	hash := strings.Repeat("0f", 16)
	for u, want := range map[string]string{
		"https://cdn-images.dzcdn.net/images/cover/" + hash + "/500x500-000000-80-0-0.jpg": "img_cover_" + hash + "_000000-80-0-0.jpg",
		"/media/covers/custom/" + hash + ".jpg":                                            "med_custom_" + hash,
		"/media/covers/" + hash + ".jpg":                                                   "med__" + hash,
		"http://example.com/x.jpg":                                                         "al9",
	} {
		id := coverID(u, "al9")
		if id != want {
			t.Errorf("coverID(%s) = %s, want %s", u, id, want)
			continue
		}
		back, direct := coverFromID(id)
		if want == "al9" {
			if direct {
				t.Errorf("%s taken for a picture", id)
			}
			continue
		}
		if wantBack := strings.Replace(u, "500x500", "1000x1000", 1); back != wantBack {
			t.Errorf("coverFromID(%s) = %s, want %s", id, back, wantBack)
		}
	}
	if u, direct := coverFromID("img_cover_../x_y"); !direct || u != "" {
		t.Errorf("bad img id: %q %v", u, direct)
	}
}

// Артист без фото — заглушка-картинка, а не ошибка (в приложении — битая картинка).
func TestSubsonicCoverPlaceholder(t *testing.T) {
	_, router, token := newTestRouter(t)
	c := newSubClient(t, router, token)
	rec := c.raw("getCoverArt", url.Values{"id": {"ar1"}, "size": {"300"}})
	if _, err := png.Decode(rec.Body); rec.Code != http.StatusOK || err != nil || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("placeholder: %d %v %q", rec.Code, err, rec.Header().Get("Content-Type"))
	}
}

// Картинка Deezer берётся с CDN один раз, дальше — из памяти.
func TestDeezerImageCached(t *testing.T) {
	var hits atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg bytes"))
	}))
	defer cdn.Close()
	old := deezerImageHost
	deezerImageHost = cdn.URL + "/images/"
	t.Cleanup(func() { deezerImageHost = old })

	router := NewRouter(&Server{})
	path := "/api/img/cover/" + strings.Repeat("1a", 16) + "/300x300-000000-80-0-0.jpg"
	for range 3 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "jpeg bytes" {
			t.Fatalf("image: %d %q", rec.Code, rec.Body.String())
		}
	}
	if hits.Load() != 1 {
		t.Errorf("CDN asked %d times, want once", hits.Load())
	}
}

func TestGuessContributors(t *testing.T) {
	album := &deezer.Album{Contributors: []deezer.ArtistRef{{ID: "1", Name: "A"}, {ID: "2", Name: "B"}}}
	tracks := []deezer.Track{
		{ID: "10", ArtistID: "1"},
		{ID: "11", ArtistID: "3"}, // чужой исполнитель — не трогаем
		{ID: "12", ArtistID: "2", Contributors: []deezer.ArtistRef{{ID: "2", Name: "B"}}}, // уже известны
	}
	guessContributors(album, tracks)
	if len(tracks[0].Contributors) != 2 || len(tracks[1].Contributors) != 0 || len(tracks[2].Contributors) != 1 {
		t.Errorf("guessed: %+v", tracks)
	}
	solo := []deezer.Track{{ID: "13", ArtistID: "1"}}
	guessContributors(&deezer.Album{Contributors: album.Contributors[:1]}, solo)
	if len(solo[0].Contributors) != 0 {
		t.Error("a solo album's artist guessed as a feature")
	}
}

// Очередь приложений занята, а приложение ушло — запрос не ждёт и не тратит лимит.
func TestSubsonicDeezerShareCancelled(t *testing.T) {
	for range cap(appSlots) {
		appSlots <- struct{}{}
	}
	defer func() {
		for range cap(appSlots) {
			<-appSlots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &subCtx{r: httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)}
	if _, err := c.deezerShare(2); err == nil {
		t.Error("waited although the app left")
	}
	if release, err := c.deezerShare(0); err != nil {
		t.Errorf("a cached album waited: %v", err)
	} else {
		release()
	}
}

// Доля приложений — 5/8 ёмкости выходов к Deezer.
func TestAppDeezerShare(t *testing.T) {
	one := &Server{Deezer: deezer.NewClient()}
	u, _ := url.Parse("socks5://127.0.0.1:40000")
	two := &Server{Deezer: deezer.NewClient().WithProxy(u, 40, 5*time.Second)}
	if one.appDeezerShare() == two.appDeezerShare() {
		t.Fatal("the same share with and without the proxy")
	}
	for _, n := range []int{25, 50} {
		if _, ok := appShares.Load(n); !ok {
			t.Errorf("no share of %d", n)
		}
	}
}
