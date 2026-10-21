package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// getJSON — GET от имени token; adminMode — с заголовком режима админа.
func getJSON(t *testing.T, router http.Handler, path, token string, adminMode bool, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if adminMode {
		req.Header.Set("X-Admin-Mode", "1")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, router http.Handler, path, token, body string, want int) {
	t.Helper()
	if rec := adminRequest(router, "PUT", path, token, []byte(body), "application/json"); rec.Code != want {
		t.Fatalf("PUT %s: %d %s, want %d", path, rec.Code, rec.Body.String(), want)
	}
}

type pageAlbum struct {
	ID         string `json:"id"`
	ArtistName string `json:"artist_name"`
	Hidden     bool   `json:"hidden"`
}

type pageTrack struct {
	ID           string `json:"id"`
	Artist       string `json:"artist"`
	ArtistID     string `json:"artist_id"`
	Explicit     bool   `json:"explicit"`
	Hidden       bool   `json:"hidden"`
	Contributors []struct {
		ID, Name string
	} `json:"contributors"`
}

func albumIDs(albums []pageAlbum) []string {
	var ids []string
	for _, a := range albums {
		ids = append(ids, a.ID)
	}
	return ids
}

// Deezer залил альбом 9 артисту 1, а он артиста 2: после правки альбом — в
// дискографии 2 и пропал у 1, его треки — от 2; вернуть как у Deezer —
// правка снимается.
func TestAlbumCredits_MoveReleaseToAnotherArtist(t *testing.T) {
	db, router, token := newTestRouter(t)
	var id string
	if err := json.Unmarshal(postJSON(router, "/api/albums/deezer/9/claim", "", token).Body.Bytes(), &struct{ ID *string }{&id}); err != nil || id == "" {
		t.Fatalf("claim: %v", err)
	}
	put(t, router, "/api/albums/"+id, token, `{"title":"Test Album","artists":["2"]}`, http.StatusOK)

	var to, from struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/2/albums", token, false, &to)
	getJSON(t, router, "/api/discover/artists/1/albums", token, false, &from)
	if !slices.Contains(albumIDs(to.Albums), "9") || to.Albums[0].ArtistName != "Other Artist" {
		t.Errorf("artist 2 discography: %+v", to.Albums)
	}
	if slices.Contains(albumIDs(from.Albums), "9") || !slices.Contains(albumIDs(from.Albums), "10") {
		t.Errorf("artist 1 discography: %+v", from.Albums)
	}
	var page struct{ Tracks []pageTrack }
	getJSON(t, router, "/api/discover/albums/9", token, false, &page)
	if tr := page.Tracks[0]; tr.ArtistID != "2" || tr.Artist != "Other Artist" {
		t.Errorf("track of the moved release: %+v", tr)
	}

	put(t, router, "/api/albums/"+id, token, `{"title":"Test Album","artists":["1"]}`, http.StatusOK)
	var ids sql.NullString
	db.QueryRow(`SELECT artist_ids::text FROM albums WHERE id = $1`, id).Scan(&ids)
	if ids.Valid {
		t.Errorf("Deezer's own artists must drop the edit, got %s", ids.String)
	}
	put(t, router, "/api/albums/"+id, token, `{"title":"Test Album","artists":["abc"]}`, http.StatusBadRequest)
}

// Исполнители и метка explicit трека — без скачивания; совпадает с Deezer —
// правки нет.
func TestTrackCredits_ArtistsAndExplicit(t *testing.T) {
	db, router, token := newTestRouter(t)
	put(t, router, "/api/tracks/deezer/12345", token, `{"title":"Test Song","explicit":true,"artists":["2","1"]}`, http.StatusOK)

	var tr pageTrack
	getJSON(t, router, "/api/discover/tracks/12345", token, false, &tr)
	if !tr.Explicit || tr.ArtistID != "2" || len(tr.Contributors) != 2 || tr.Contributors[1].Name != "Test Artist" {
		t.Errorf("edited track: %+v", tr)
	}
	var edit struct {
		Edited        bool `json:"edited"`
		CreditsEdited bool `json:"credits_edited"`
		Explicit      bool `json:"explicit"`
	}
	getJSON(t, router, "/api/tracks/deezer/12345/edit", token, false, &edit)
	if !edit.Edited || !edit.CreditsEdited || !edit.Explicit {
		t.Errorf("edit state: %+v", edit)
	}

	put(t, router, "/api/tracks/deezer/12345", token, `{"title":"Test Song","explicit":false,"artists":["1"]}`, http.StatusOK)
	var n int
	db.QueryRow(`SELECT count(*) FROM track_overrides WHERE deezer_id = '12345'`).Scan(&n)
	if n != 0 {
		t.Error("an edit equal to Deezer must be dropped")
	}
}

// Скрытый релиз пропадает из дискографии и топа, кроме режима админа; в
// любимых его трек остаётся с пометкой.
func TestHiddenRelease(t *testing.T) {
	_, router, token := newTestRouter(t)
	put(t, router, "/api/me/liked/12345", token, ``, http.StatusNoContent)
	put(t, router, "/api/albums/deezer/9/hidden", token, `{"hidden":true}`, http.StatusOK)

	var page struct {
		Albums []pageAlbum
		Top    []pageTrack
	}
	getJSON(t, router, "/api/discover/artists/1", token, false, &page)
	if slices.Contains(albumIDs(page.Albums), "9") || len(page.Top) != 0 {
		t.Errorf("hidden release shown: %+v %+v", page.Albums, page.Top)
	}
	getJSON(t, router, "/api/discover/artists/1", token, true, &page)
	if i := slices.Index(albumIDs(page.Albums), "9"); i < 0 || !page.Albums[i].Hidden || len(page.Top) != 1 {
		t.Errorf("admin mode must show it marked: %+v %+v", page.Albums, page.Top)
	}
	var liked struct{ Tracks []pageTrack }
	getJSON(t, router, "/api/me/liked", token, false, &liked)
	if len(liked.Tracks) != 1 || !liked.Tracks[0].Hidden {
		t.Errorf("liked track of a hidden release: %+v", liked.Tracks)
	}
}

// Перекачать можно только трек из медиатеки и не дважды сразу.
func TestRedownloadTrack(t *testing.T) {
	db, router, token := newTestRouter(t)
	post := func(want int) {
		t.Helper()
		if rec := postJSON(router, "/api/tracks/deezer/12345/redownload", "", token); rec.Code != want {
			t.Fatalf("redownload: %d %s, want %d", rec.Code, rec.Body.String(), want)
		}
	}
	post(http.StatusConflict) // нет в медиатеке

	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Test Song', 200000, 'old.m4a', '12345')`, albumID, artistID)
	db.Exec(`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status, video_id)
	         VALUES ('deezer:12345', 'Test Song', 'Test Artist', 'Test Artist', 'Test Album', 200000, 'done', 'wrong')`)
	post(http.StatusAccepted)
	var status string
	var replace bool
	var video sql.NullString
	var explicit sql.NullBool
	db.QueryRow(`SELECT status, replace_audio, video_id, explicit FROM ingest_jobs WHERE source_ref = 'deezer:12345'`).Scan(&status, &replace, &video, &explicit)
	if status != "pending" || !replace || video.Valid {
		t.Errorf("job: %s replace=%v video=%v", status, replace, video)
	}
	if !explicit.Valid || explicit.Bool { // задача без explicit — метку берём у Deezer
		t.Errorf("explicit = %v, want false from Deezer", explicit)
	}
	post(http.StatusConflict) // уже в очереди
}

// «Похоже на …» — с именем, исправленным админом, а не из истории.
func TestSuggestionsBasedOnUsesEditedNames(t *testing.T) {
	db, router, token := newTestRouter(t)
	var userID string
	db.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID)
	db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id) VALUES ($1, '12345', '{"artist_id":"1","artist":"Old Name"}', 'album', '9')`, userID)
	db.Exec(`INSERT INTO artists (name, deezer_id, edited) VALUES ('New Name', '1', true)`)
	var got struct {
		BasedOn []string `json:"based_on"`
	}
	getJSON(t, router, "/api/me/suggestions", token, false, &got)
	if !slices.Equal(got.BasedOn, []string{"New Name"}) {
		t.Errorf("based_on = %v", got.BasedOn)
	}
}

// Правка исполнителей релиза доходит до трека, только если на треке был
// убранный артист: остальные исполнители трека не трогаются.
func TestReplaceCredits(t *testing.T) {
	cases := []struct {
		cur, from, to, want []string
	}{
		{[]string{"dup"}, []string{"dup"}, []string{"real"}, []string{"real"}},                   // дубликат → настоящий
		{[]string{"dup", "x"}, []string{"dup"}, []string{"real"}, []string{"real", "x"}},         // фит остаётся
		{[]string{"a", "x"}, []string{"a", "b", "c"}, []string{"a", "b", "d"}, nil},              // трек без убранного C
		{[]string{"c"}, []string{"a", "b", "c"}, []string{"a", "b", "d"}, []string{"d"}},         // C → D
		{[]string{"a", "b"}, []string{"a", "b"}, []string{"b", "a"}, nil},                        // только порядок
		{[]string{"a"}, []string{"a"}, []string{"a", "b"}, nil},                                  // только добавили
		{[]string{"x", "dup", "real"}, []string{"dup"}, []string{"real"}, []string{"x", "real"}}, // без повторов
	}
	for _, c := range cases {
		if got := replaceCredits(c.cur, c.from, c.to); !slices.Equal(got, c.want) {
			t.Errorf("replaceCredits(%v, %v → %v) = %v, want %v", c.cur, c.from, c.to, got, c.want)
		}
	}
}

// «Популярное» — сначала то, что слушали здесь за месяц (больше — выше),
// потом топ Deezer без повторов.
func TestArtistTopByPlaysOnTheService(t *testing.T) {
	db, router, token := newTestRouter(t)
	var userID string
	db.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&userID)
	play := func(id, meta string, n int, ago string) {
		for i := 0; i < n; i++ {
			db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, played_at)
			         VALUES ($1, $2, $3, 'album', '9', now() - $4::interval)`, userID, id, meta, ago)
		}
	}
	play("501", `{"id":"501","title":"Album Track One","artist_id":"1","artist":"Test Artist","album_id":"77"}`, 1, "1 day")
	play("12345", `{"id":"12345","title":"Test Song","artist_id":"9","artist":"Someone","album_id":"9",
		"contributors":[{"id":"9","name":"Someone"},{"id":"1","name":"Test Artist"}]}`, 3, "2 days") // фит
	play("777", `{"id":"777","title":"Old","artist_id":"1","artist":"Test Artist","album_id":"9"}`, 5, "40 days") // давно

	var page struct{ Top []pageTrack }
	getJSON(t, router, "/api/discover/artists/1", token, false, &page)
	var ids []string
	for _, tr := range page.Top {
		ids = append(ids, tr.ID)
	}
	if !slices.Equal(ids, []string{"12345", "501", "888"}) {
		t.Errorf("top = %v, want plays first (12345, 501), then Deezer's (888)", ids)
	}
}

// wavTone — секунда синуса в WAV (16 бит, моно, 8 кГц).
func wavTone() []byte {
	const rate, n = 8000, 8000
	b := new(bytes.Buffer)
	le := func(v any) { binary.Write(b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + n*2))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1)) // PCM
	le(uint16(1))
	le(uint32(rate))
	le(uint32(rate * 2))
	le(uint16(2))
	le(uint16(16))
	b.WriteString("data")
	le(uint32(n * 2))
	for i := 0; i < n; i++ {
		le(int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate)))
	}
	return b.Bytes()
}

// Свой файл заменяет аудио трека и той же записи на другом релизе (файл
// общий); старый файл удаляется. Не аудио и трек не из медиатеки — отказ.
func TestUploadTrackAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	db, router, token, srv := newTestServer(t)
	storage := t.TempDir()
	srv.StorageDir = storage
	upload := func(body []byte, want int) {
		t.Helper()
		if rec := adminRequest(router, "PUT", "/api/tracks/deezer/12345/audio", token, body, "audio/wav"); rec.Code != want {
			t.Fatalf("upload: %d %s, want %d", rec.Code, rec.Body.String(), want)
		}
	}
	upload(wavTone(), http.StatusConflict) // нет в медиатеке

	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Test Song', 200000, 'old.m4a', '12345')`, albumID, artistID)
	db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Test Song (single)', 200000, 'old.m4a', '12346')`, albumID, artistID)
	os.WriteFile(filepath.Join(storage, "old.m4a"), []byte("old"), 0o644)

	upload([]byte("not audio at all"), http.StatusBadRequest)
	upload(wavTone(), http.StatusOK)

	rows, _ := db.Query(`SELECT DISTINCT storage_key FROM tracks WHERE deezer_id IN ('12345', '12346')`)
	var keys []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	if len(keys) != 1 || keys[0] == "old.m4a" {
		t.Fatalf("storage keys after upload: %v", keys)
	}
	if _, err := os.Stat(filepath.Join(storage, keys[0])); err != nil {
		t.Errorf("new file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage, "old.m4a")); !os.IsNotExist(err) {
		t.Errorf("old file must be removed: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(storage, ".upload-*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}

	// Пока трек перекачивается, загрузка подождёт.
	db.Exec(`INSERT INTO ingest_jobs (source_ref, title, artist_name, album_artist_name, album_title, duration_ms, status)
	         VALUES ('deezer:12345', 'Test Song', 'Test Artist', 'Test Artist', 'Test Album', 200000, 'pending')`)
	upload(wavTone(), http.StatusConflict)
}

// Перекодирование своего файла бывает дольше общих таймаутов сервера
// (cmd/api): ответ всё равно доходит. Раньше соединение обрывалось, и админ
// видел 502 от nginx, хотя файл уже заменился.
func TestUploadTrackAudio_OutlivesServerTimeouts(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	db, router, token, srv := newTestServer(t)
	srv.StorageDir = t.TempDir()
	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Test Artist', '1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Test Album', '9') RETURNING id`, artistID).Scan(&albumID)
	db.Exec(`INSERT INTO tracks (album_id, artist_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, $2, 'Test Song', 200000, 'old.m4a', '12345')`, albumID, artistID)

	orig := prepareAudio
	prepareAudio = func(ctx context.Context, src, dst string) (float64, float64, error) {
		time.Sleep(300 * time.Millisecond) // длинный FLAC на одном ядре
		return orig(ctx, src, dst)
	}
	t.Cleanup(func() { prepareAudio = orig })
	ts := httptest.NewUnstartedServer(router)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/tracks/deezer/12345/audio", bytes.NewReader(wavTone()))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "audio/wav")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: connection cut: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
}
