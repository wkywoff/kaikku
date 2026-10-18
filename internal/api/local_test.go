package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"kaikku/internal/lyrics"
	"kaikku/internal/media"
)

// ownRelease — свой релиз в базе, как его оставляет загрузка (uploads.go).
type ownRelease struct {
	artist, album string   // k-ID
	tracks        []string // k-ID по порядку
}

// seedOwnRelease заводит своего артиста и релиз (исполнители — он и артист
// Deezer 1) с двумя треками; второй — с фитом артиста Deezer 2 и explicit.
// owner — ID пользователя или "".
func seedOwnRelease(t *testing.T, db *sql.DB, owner string, draft bool) ownRelease {
	t.Helper()
	var rel ownRelease
	next := func() string {
		var id string
		if err := db.QueryRow(`SELECT 'k' || nextval('kaikku_ids')`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	rel.artist, rel.album = next(), next()
	rel.tracks = []string{next(), next()}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO artists (deezer_id, name, edited, owner_id) VALUES ($1, 'Own Artist', true, $2)`, rel.artist, nullableString(owner))
	exec(`INSERT INTO artists (deezer_id, name) VALUES ('1', 'Test Artist'), ('2', 'Other Artist') ON CONFLICT (deezer_id) DO NOTHING`)
	var albumID string
	if err := db.QueryRow(
		`INSERT INTO albums (deezer_id, title, released_at, record_type, cover_key, cover_custom, edited, artist_ids, owner_id, draft)
		 VALUES ($1, 'Own EP', '2026-10-01', 'ep', 'covers/custom/00112233445566778899aabbccddeeff.jpg', true, true, $2, $3, $4)
		 RETURNING id`, rel.album, "{"+rel.artist+",1}", nullableString(owner), draft).Scan(&albumID); err != nil {
		t.Fatal(err)
	}
	for i, id := range rel.tracks {
		exec(`INSERT INTO tracks (deezer_id, album_id, title, duration_ms, track_no, disc_no, storage_key, isrc, edited)
		      VALUES ($1, $2, $3, $4, $5, 1, $6, '', true)`,
			id, albumID, []string{"First", "Second"}[i], 60000*(i+1), i+1, "uploads/"+id+".m4a")
	}
	exec(`INSERT INTO track_overrides (deezer_id, explicit, artist_ids) VALUES ($1, true, $2)`, rel.tracks[1], "{"+rel.artist+",2}")
	return rel
}

type localAlbumPage struct {
	Album struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		ArtistID     string `json:"artist_id"`
		ArtistName   string `json:"artist_name"`
		CoverURL     string `json:"cover_url"`
		RecordType   string `json:"record_type"`
		NbTracks     int    `json:"nb_tracks"`
		Explicit     bool   `json:"explicit"`
		Hidden       bool   `json:"hidden"`
		Contributors []struct {
			ID, Name string
		} `json:"contributors"`
	} `json:"album"`
	Tracks   []pageTrack `json:"tracks"`
	Draft    bool        `json:"draft"`
	Editable bool        `json:"editable"`
}

func TestOwnRelease_PublishedVisibleToAll(t *testing.T) {
	db, router, admin := newTestRouter(t)
	other := invitedUser(t, router, admin, "listener")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)

	var page localAlbumPage
	getJSON(t, router, "/api/discover/albums/"+rel.album, other, false, &page)
	a := page.Album
	if a.Title != "Own EP" || a.RecordType != "ep" || a.NbTracks != 2 || !a.Explicit || a.Hidden {
		t.Errorf("album = %+v", a)
	}
	if a.CoverURL != "/media/covers/custom/00112233445566778899aabbccddeeff.jpg" {
		t.Errorf("cover = %q", a.CoverURL)
	}
	if a.ArtistID != rel.artist || a.ArtistName != "Own Artist" || len(a.Contributors) != 2 || a.Contributors[1].Name != "Test Artist" {
		t.Errorf("artists = %s %s %+v", a.ArtistID, a.ArtistName, a.Contributors)
	}
	if page.Draft || page.Editable {
		t.Errorf("draft/editable for a listener: %v %v", page.Draft, page.Editable)
	}
	if len(page.Tracks) != 2 || page.Tracks[0].ID != rel.tracks[0] || page.Tracks[0].Explicit {
		t.Fatalf("tracks = %+v", page.Tracks)
	}
	if c := page.Tracks[0].Contributors; len(c) != 2 || c[0].ID != rel.artist {
		t.Errorf("first track artists = %+v, want the release's", c)
	}
	if c := page.Tracks[1].Contributors; !page.Tracks[1].Explicit || len(c) != 2 || c[1].Name != "Other Artist" {
		t.Errorf("second track = %+v", page.Tracks[1])
	}

	// Карандаш на странице релиза — только в режиме админа, даже автору
	// (свои релизы правят из медиатеки).
	getJSON(t, router, "/api/discover/albums/"+rel.album, admin, false, &page)
	if page.Editable {
		t.Error("editable without admin mode")
	}
	getJSON(t, router, "/api/discover/albums/"+rel.album, admin, true, &page)
	if !page.Editable {
		t.Error("admin mode should get editable")
	}

	var track pageTrack
	getJSON(t, router, "/api/discover/tracks/"+rel.tracks[1], other, false, &track)
	if track.ID != rel.tracks[1] || !track.Explicit || track.ArtistID != rel.artist {
		t.Errorf("track = %+v", track)
	}
}

func TestOwnRelease_DraftOnlyForOwnerAndAdmin(t *testing.T) {
	db, router, admin := newTestRouter(t)
	author := invitedUser(t, router, admin, "author")
	other := invitedUser(t, router, admin, "stranger")
	rel := seedOwnRelease(t, db, userIDOf(t, router, author), true)

	for _, path := range []string{"/api/discover/albums/" + rel.album, "/api/discover/tracks/" + rel.tracks[0]} {
		call(t, router, "GET", path, other, "", http.StatusNotFound)
		call(t, router, "GET", path, admin, "", http.StatusNotFound) // админ — только в режиме админа
	}

	var page localAlbumPage
	getJSON(t, router, "/api/discover/albums/"+rel.album, author, false, &page)
	if !page.Draft || page.Editable || page.Album.Hidden || page.Tracks[0].Hidden {
		t.Errorf("author's draft: draft=%v editable=%v hidden=%v/%v", page.Draft, page.Editable, page.Album.Hidden, page.Tracks[0].Hidden)
	}
	getJSON(t, router, "/api/discover/albums/"+rel.album, admin, true, &page)
	if !page.Editable {
		t.Error("admin in admin mode should get editable")
	}

	// В дискографии артиста Deezer черновика нет.
	var artist struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/1", other, false, &artist)
	if slices.ContainsFunc(artist.Albums, func(a pageAlbum) bool { return a.ID == rel.album }) {
		t.Error("draft in the Deezer artist's discography")
	}
}

func TestOwnRelease_InDeezerArtistDiscography(t *testing.T) {
	db, router, admin := newTestRouter(t)
	rel := seedOwnRelease(t, db, "", false)

	var page struct{ Albums []pageAlbum }
	getJSON(t, router, "/api/discover/artists/1", admin, false, &page)
	i := slices.IndexFunc(page.Albums, func(a pageAlbum) bool { return a.ID == rel.album })
	if i < 0 {
		t.Fatalf("own release missing from artist 1: %+v", page.Albums)
	}
	if page.Albums[i].ArtistName != "Own Artist" {
		t.Errorf("artist name = %q", page.Albums[i].ArtistName)
	}
	getJSON(t, router, "/api/discover/artists/1/albums", admin, false, &page)
	if !slices.ContainsFunc(page.Albums, func(a pageAlbum) bool { return a.ID == rel.album }) {
		t.Error("own release missing from /albums")
	}
}

func TestOwnArtistPage(t *testing.T) {
	db, router, admin := newTestRouter(t)
	other := invitedUser(t, router, admin, "fan")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)

	var page struct {
		Artist struct {
			ID, Name string
		}
		Albums   []pageAlbum
		Top      []pageTrack
		Editable bool
	}
	getJSON(t, router, "/api/discover/artists/"+rel.artist, other, false, &page)
	if page.Artist.Name != "Own Artist" || len(page.Albums) != 1 || page.Albums[0].ID != rel.album || len(page.Top) != 2 || page.Editable {
		t.Errorf("page = %+v", page)
	}
	getJSON(t, router, "/api/discover/artists/"+rel.artist, admin, false, &page)
	if !page.Editable {
		t.Error("the artist's creator should get editable")
	}

	var related struct{ Artists []any }
	getJSON(t, router, "/api/discover/artists/"+rel.artist+"/related", other, false, &related)
	if related.Artists == nil || len(related.Artists) != 0 {
		t.Errorf("related = %+v", related.Artists)
	}
	var radio struct{ Tracks []pageTrack }
	getJSON(t, router, "/api/discover/artists/"+rel.artist+"/radio", other, false, &radio)
	if len(radio.Tracks) != 2 {
		t.Errorf("radio = %+v", radio.Tracks)
	}
	var info struct{ ID, Name string }
	getJSON(t, router, "/api/discover/artists/"+rel.artist+"/info", other, false, &info)
	if info.Name != "Own Artist" {
		t.Errorf("info = %+v", info)
	}
	call(t, router, "GET", "/api/discover/artists/k999999/info", other, "", http.StatusNotFound)
}

// Прослушивания черновика не попадают в «Популярное» артиста Deezer.
func TestOwnRelease_DraftPlaysNotPopular(t *testing.T) {
	db, router, admin := newTestRouter(t)
	author := invitedUser(t, router, admin, "author2")
	rel := seedOwnRelease(t, db, userIDOf(t, router, author), true)
	meta, _ := json.Marshal(map[string]any{"id": rel.tracks[0], "title": "First", "artist_id": "1", "album_id": rel.album})
	if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id)
		VALUES ($1, $2, $3, 'album', $4)`, userIDOf(t, router, author), rel.tracks[0], meta, rel.album); err != nil {
		t.Fatal(err)
	}
	var page struct{ Top []pageTrack }
	getJSON(t, router, "/api/discover/artists/1", admin, false, &page)
	if slices.ContainsFunc(page.Top, func(t pageTrack) bool { return t.ID == rel.tracks[0] }) {
		t.Error("a draft's track is in Popular")
	}
}

// Админские инструменты для строк Deezer не трогают свои релизы.
func TestOwnRelease_AdminDeezerToolsRefuse(t *testing.T) {
	db, router, admin := newTestRouter(t)
	rel := seedOwnRelease(t, db, "", false)
	var albumID, artistID, trackID string
	db.QueryRow(`SELECT id FROM albums WHERE deezer_id = $1`, rel.album).Scan(&albumID)
	db.QueryRow(`SELECT id FROM artists WHERE deezer_id = $1`, rel.artist).Scan(&artistID)
	db.QueryRow(`SELECT id FROM tracks WHERE deezer_id = $1`, rel.tracks[0]).Scan(&trackID)
	call(t, router, "GET", "/api/albums/"+albumID, admin, "", http.StatusBadRequest)
	call(t, router, "PUT", "/api/albums/"+albumID, admin, `{"title":"X"}`, http.StatusBadRequest)
	call(t, router, "POST", "/api/albums/"+albumID+"/reset", admin, "", http.StatusBadRequest)
	call(t, router, "DELETE", "/api/albums/"+albumID, admin, "", http.StatusBadRequest)
	call(t, router, "POST", "/api/artists/"+artistID+"/reset", admin, "", http.StatusBadRequest)
	call(t, router, "DELETE", "/api/tracks/"+trackID, admin, "", http.StatusBadRequest)
}

// Лайк, сохранение, подписка и плейлист со своими ID: снимки — из базы,
// на скачивание ничего не ставится, правки автора видны в медиатеке.
func TestOwnRelease_LibraryActions(t *testing.T) {
	db, router, admin := newTestRouter(t)
	user := invitedUser(t, router, admin, "collector")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)

	call(t, router, "PUT", "/api/me/liked/"+rel.tracks[1], user, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/"+rel.album, user, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/artists/"+rel.artist, user, "", http.StatusNoContent)
	var p struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", user, `{"title":"Mixed"}`, http.StatusCreated), &p)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", user,
		`{"deezer_ids":["`+rel.tracks[0]+`","12345","`+rel.tracks[1]+`"]}`, http.StatusOK)
	call(t, router, "POST", "/api/playlists/"+p.ID+"/tracks", user, `{"deezer_ids":["k999999"]}`, http.StatusNotFound)

	var jobs int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE source_ref NOT IN ('deezer:12345')`).Scan(&jobs)
	if jobs != 0 {
		t.Errorf("%d download jobs for own uploads", jobs)
	}

	var playlist struct {
		Tracks []struct {
			ID, Title string
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/playlists/"+p.ID, user, "", http.StatusOK), &playlist)
	var order []string
	for _, tr := range playlist.Tracks {
		order = append(order, tr.ID)
	}
	if !slices.Equal(order, []string{rel.tracks[0], "12345", rel.tracks[1]}) {
		t.Errorf("playlist order = %v", order)
	}

	// Автор переименовал релиз и трек — в медиатеке новые названия.
	db.Exec(`UPDATE albums SET title = 'Renamed EP' WHERE deezer_id = $1`, rel.album)
	db.Exec(`UPDATE tracks SET title = 'Renamed song' WHERE deezer_id = $1`, rel.tracks[1])
	var liked struct {
		Tracks []struct {
			ID, Title, Album string
			Explicit         bool
			Hidden           bool
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/liked", user, "", http.StatusOK), &liked)
	if len(liked.Tracks) != 1 || liked.Tracks[0].Title != "Renamed song" || liked.Tracks[0].Album != "Renamed EP" || !liked.Tracks[0].Explicit {
		t.Errorf("liked = %+v", liked.Tracks)
	}
	// Снят с публикации — у остальных серым.
	db.Exec(`UPDATE albums SET draft = true WHERE deezer_id = $1`, rel.album)
	json.Unmarshal(call(t, router, "GET", "/api/me/liked", user, "", http.StatusOK), &liked)
	if !liked.Tracks[0].Hidden {
		t.Error("an unpublished release's track should be hidden in likes")
	}
	call(t, router, "PUT", "/api/me/liked/"+rel.tracks[0], user, "", http.StatusNotFound)
}

func TestOwnRelease_PlaysAndNewReleases(t *testing.T) {
	db, router, admin := newTestRouter(t)
	user := invitedUser(t, router, admin, "listener2")
	rel := seedOwnRelease(t, db, "", false)
	db.Exec(`UPDATE albums SET released_at = current_date - 3 WHERE deezer_id = $1`, rel.album)

	call(t, router, "POST", "/api/me/plays", user,
		`{"deezer_id":"`+rel.tracks[0]+`","context":{"kind":"artist","id":"`+rel.artist+`"}}`, http.StatusNoContent)
	call(t, router, "POST", "/api/me/plays", user,
		`{"deezer_id":"`+rel.tracks[1]+`","context":{"kind":"album","id":"`+rel.album+`"}}`, http.StatusNoContent)
	var recent struct {
		Items []struct {
			Kind   string
			Album  *struct{ ID, Title string }
			Artist *struct{ ID, Name string }
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", user, "", http.StatusOK), &recent)
	if len(recent.Items) != 2 || recent.Items[0].Album == nil || recent.Items[0].Album.ID != rel.album ||
		recent.Items[1].Artist == nil || recent.Items[1].Artist.Name != "Own Artist" {
		t.Errorf("recent = %+v", recent.Items)
	}

	call(t, router, "PUT", "/api/me/artists/"+rel.artist, user, "", http.StatusNoContent)
	var releases struct{ Albums []pageAlbum }
	json.Unmarshal(call(t, router, "GET", "/api/me/releases", user, "", http.StatusOK), &releases)
	if !slices.ContainsFunc(releases.Albums, func(a pageAlbum) bool { return a.ID == rel.album }) {
		t.Errorf("own artist's new release missing: %+v", releases.Albums)
	}
	call(t, router, "GET", "/api/me/suggestions", user, "", http.StatusOK)
}

func TestOwnRelease_StreamLookupLyricsSearch(t *testing.T) {
	db, router, admin, srv := newTestServer(t)
	srv.MediaSigner = &media.Signer{Secret: "s3cret", TTL: time.Minute}
	srv.Lyrics = &lyrics.Service{DB: db, Providers: []lyrics.Provider{failProvider{t}}}
	stranger := invitedUser(t, router, admin, "stranger3")
	pub := seedOwnRelease(t, db, "", false)
	draft := seedOwnRelease(t, db, userIDOf(t, router, admin), true)
	db.Exec(`UPDATE albums SET title = 'Secret Draft' WHERE deezer_id = $1`, draft.album)

	var stream struct {
		URL     string `json:"url"`
		TrackID string `json:"track_id"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/stream/deezer/"+pub.tracks[0]+"/url", stranger, "", http.StatusOK), &stream)
	if !strings.HasPrefix(stream.URL, "/media/uploads/"+pub.tracks[0]+".m4a?") || stream.TrackID == "" {
		t.Errorf("stream = %+v", stream)
	}
	call(t, router, "GET", "/api/stream/deezer/"+draft.tracks[0]+"/url", stranger, "", http.StatusNotFound)
	call(t, router, "GET", "/api/stream/deezer/"+draft.tracks[0]+"/url", admin, "", http.StatusOK)
	var draftTrackID string
	db.QueryRow(`SELECT id FROM tracks WHERE deezer_id = $1`, draft.tracks[0]).Scan(&draftTrackID)
	call(t, router, "GET", "/api/tracks/"+draftTrackID+"/stream-url", stranger, "", http.StatusNotFound)
	var jobs int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs`).Scan(&jobs)
	if jobs != 0 {
		t.Errorf("%d jobs", jobs)
	}

	lookup := map[string]struct {
		TrackID string `json:"track_id"`
	}{}
	json.Unmarshal(call(t, router, "GET", "/api/ingest/lookup?ids=12345,"+pub.tracks[0]+","+draft.tracks[0], stranger, "", http.StatusOK), &lookup)
	if lookup[pub.tracks[0]].TrackID == "" || len(lookup) != 1 {
		t.Errorf("lookup = %+v", lookup)
	}

	body := []byte(`{"source":"upload","sync":"none","lines":[{"s":0,"e":0,"t":"la la"}]}`)
	db.Exec(`INSERT INTO lyrics (deezer_id, source, sync, body, fetched_at) VALUES ($1, 'upload', 'none', $2, now() - interval '1 year')`, pub.tracks[0], body)
	var got struct {
		Lyrics *struct {
			Lines []struct{ T string }
		}
	}
	json.Unmarshal(call(t, router, "GET", "/api/lyrics/"+pub.tracks[0], stranger, "", http.StatusOK), &got)
	if got.Lyrics == nil || len(got.Lyrics.Lines) != 1 || got.Lyrics.Lines[0].T != "la la" {
		t.Errorf("own lyrics = %+v", got.Lyrics)
	}
	json.Unmarshal(call(t, router, "GET", "/api/lyrics/"+pub.tracks[1], stranger, "", http.StatusOK), &got)
	if got.Lyrics != nil {
		t.Errorf("no lyrics expected, got %+v", got.Lyrics)
	}
	call(t, router, "GET", "/api/lyrics/"+draft.tracks[0], stranger, "", http.StatusNotFound)

	var found struct {
		Tracks  []pageTrack
		Albums  []pageAlbum
		Artists []struct{ ID, Name string }
	}
	json.Unmarshal(call(t, router, "GET", "/api/search?q=own", stranger, "", http.StatusOK), &found)
	if len(found.Albums) == 0 || found.Albums[0].ID != pub.album || len(found.Artists) == 0 || found.Artists[0].Name != "Own Artist" {
		t.Errorf("search: albums %+v artists %+v", found.Albums, found.Artists)
	}
	json.Unmarshal(call(t, router, "GET", "/api/search?q=secret", stranger, "", http.StatusOK), &found)
	if slices.ContainsFunc(found.Albums, func(a pageAlbum) bool { return a.ID == draft.album }) {
		t.Error("a draft is found by search")
	}
	json.Unmarshal(call(t, router, "GET", "/api/search?q=FIRST", stranger, "", http.StatusOK), &found)
	if len(found.Tracks) == 0 || found.Tracks[0].ID != pub.tracks[0] {
		t.Errorf("track search = %+v", found.Tracks)
	}

	var picker struct{ Artists []struct{ ID, Name string } }
	json.Unmarshal(call(t, router, "GET", "/api/search/artists?q=own", admin, "", http.StatusOK), &picker)
	if len(picker.Artists) < 2 || picker.Artists[0].Name != "Own Artist" {
		t.Errorf("picker = %+v", picker.Artists)
	}
}

// failProvider — источник текстов, к которому обращаться нельзя.
type failProvider struct{ t *testing.T }

func (failProvider) Name() string { return "fail" }
func (p failProvider) Find(_ context.Context, tr lyrics.Track) (*lyrics.Lyrics, error) {
	p.t.Errorf("lyrics provider asked about %s", tr.DeezerID)
	return nil, nil
}

// Убрали артиста из трека — в его «Популярном» трека больше нет, хотя
// прослушивания записаны, пока он там был.
func TestPopular_FollowsCurrentCredits(t *testing.T) {
	db, router, admin := newTestRouter(t)
	rel := seedOwnRelease(t, db, "", false)
	meta, _ := json.Marshal(map[string]any{"id": rel.tracks[0], "title": "First", "artist_id": rel.artist, "album_id": rel.album,
		"contributors": []map[string]string{{"id": rel.artist, "name": "Own Artist"}, {"id": "2", "name": "Other Artist"}}})
	if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id) VALUES ($1, $2, $3, 'album', $4)`,
		userIDOf(t, router, admin), rel.tracks[0], meta, rel.album); err != nil {
		t.Fatal(err)
	}
	var page struct{ Top []pageTrack }
	getJSON(t, router, "/api/discover/artists/2", admin, false, &page)
	if len(page.Top) != 0 {
		t.Errorf("artist 2 popular = %+v, want nothing: the track no longer credits them", page.Top)
	}
	getJSON(t, router, "/api/discover/artists/"+rel.artist, admin, false, &page)
	if len(page.Top) == 0 || page.Top[0].ID != rel.tracks[0] {
		t.Errorf("own artist popular = %+v", page.Top)
	}
}
