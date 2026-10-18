package deezer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Формы ответов ниже повторяют реальные ответы Deezer API, урезанные до
// используемых полей.

const sampleSearchResponse = `{
  "data": [
    {
      "id": 3135556,
      "title": "Harder, Better, Faster, Stronger",
      "duration": 224,
      "track_position": 2,
      "artist": {"id": 27, "name": "Daft Punk"},
      "album": {"id": 302127, "title": "Discovery", "cover_xl": "https://cdn.example/xl.jpg", "cover_big": "https://cdn.example/big.jpg"}
    }
  ],
  "total": 1
}`

const sampleArtistAlbumsResponse = `{
  "data": [
    {"id": 302127, "title": "Discovery", "cover_xl": "https://cdn.example/discovery.jpg", "release_date": "2001-03-07"},
    {"id": 302128, "title": "Homework", "cover_xl": "https://cdn.example/homework.jpg", "release_date": "1997-01-20"}
  ],
  "total": 2
}`

const sampleAlbumTracksResponse = `{
  "data": [
    {"id": 3135553, "title": "One More Time", "duration": 320, "track_position": 1, "disk_number": 1, "artist": {"id": 27, "name": "Daft Punk"}},
    {"id": 3135556, "title": "Harder, Better, Faster, Stronger", "duration": 224, "track_position": 2, "disk_number": 1, "artist": {"id": 27, "name": "Daft Punk"}}
  ],
  "total": 2
}`

var trackCalls int

func mockDeezerServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Error("search request missing q param")
		}
		w.Write([]byte(sampleSearchResponse))
	})
	mux.HandleFunc("/track/3135556", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.Unmarshal([]byte(sampleSearchResponse), &body)
		items := body["data"].([]any)
		out, _ := json.Marshal(items[0])
		w.Write(out)
	})
	mux.HandleFunc("/artist/27/albums", func(w http.ResponseWriter, r *http.Request) {
		// Намеренно НЕ включаем artist-ref в каждый альбом — как в реальном
		// Deezer-ответе на /artist/{id}/albums (см. package doc, "implied" parent).
		w.Write([]byte(sampleArtistAlbumsResponse))
	})
	mux.HandleFunc("/album/302127/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleAlbumTracksResponse))
	})
	// Реальный ответ Deezer на несуществующий id: HTTP 200 + error-объект.
	mux.HandleFunc("/track/999", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	})
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[],"total":236}`))
	})
	mux.HandleFunc("/track/3135553", func(w http.ResponseWriter, r *http.Request) {
		trackCalls++
		w.Write([]byte(`{"id":3135553,"title":"One More Time","duration":320,"artist":{"id":27,"name":"Daft Punk"},
			"contributors":[{"id":27,"name":"Daft Punk","role":"Main"},{"id":555,"name":"Romanthony","role":"Featured"},{"id":9,"name":"Composer Guy","role":"Composer"}]}`))
	})
	mux.HandleFunc("/search/album", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":6575789,"title":"Random Access Memories","cover_xl":"https://cdn.example/ram.jpg","record_type":"album","nb_tracks":13,"artist":{"id":27,"name":"Daft Punk"}}],"total":1}`))
	})
	mux.HandleFunc("/artist/27/top", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":67238735,"title":"Instant Crush (feat. Julian Casablancas)","duration":337,"explicit_lyrics":false,
			"artist":{"id":27,"name":"Daft Punk"},"album":{"id":6575789,"title":"Random Access Memories","cover_xl":"https://cdn.example/ram.jpg"},
			"contributors":[{"id":27,"name":"Daft Punk"},{"id":1188,"name":"Julian Casablancas"}]}],"total":1}`))
	})
	return httptest.NewServer(mux)
}

func TestClient_Search(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	tracks, err := c.Search("harder better", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	tr := tracks[0]
	if tr.ID != "3135556" || tr.Title != "Harder, Better, Faster, Stronger" {
		t.Errorf("unexpected track: %+v", tr)
	}
	if tr.Artist != "Daft Punk" || tr.ArtistID != "27" {
		t.Errorf("unexpected artist fields: %+v", tr)
	}
	if tr.Album != "Discovery" || tr.AlbumID != "302127" {
		t.Errorf("unexpected album fields: %+v", tr)
	}
	if tr.DurationMs != 224000 {
		t.Errorf("DurationMs = %d, want 224000", tr.DurationMs)
	}
	if tr.CoverURL != "https://cdn.example/xl.jpg" {
		t.Errorf("CoverURL = %q, want cover_xl to be preferred", tr.CoverURL)
	}
	if tr.TrackNo != 2 {
		t.Errorf("TrackNo = %d, want 2", tr.TrackNo)
	}
}

func TestClient_GetTrack(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	tr, err := c.GetTrack("3135556")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if tr.Title != "Harder, Better, Faster, Stronger" {
		t.Errorf("unexpected track: %+v", tr)
	}
}

func TestClient_GetArtistAlbums_FillsInArtistIDFromParam(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	albums, err := c.GetArtistAlbums("27", 10)
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("got %d albums, want 2", len(albums))
	}
	for _, a := range albums {
		if a.ArtistID != "27" {
			t.Errorf("album %q: ArtistID = %q, want %q (should be filled from the id param, not the response)", a.Title, a.ArtistID, "27")
		}
	}
	if albums[0].Title != "Discovery" || albums[0].CoverURL != "https://cdn.example/discovery.jpg" {
		t.Errorf("unexpected first album: %+v", albums[0])
	}
}

func TestClient_GetAlbumTracks_FillsInAlbumFields(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	album := &Album{ID: "302127", Title: "Discovery", CoverURL: "https://cdn.example/discovery.jpg"}
	tracks, err := c.GetAlbumTracks(album, 10)
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}
	for _, tr := range tracks {
		// В ответе /album/{id}/tracks нет объекта album — всё берётся из album.
		if tr.AlbumID != "302127" || tr.Album != "Discovery" || tr.CoverURL != album.CoverURL {
			t.Errorf("track %q: album fields not filled from album: %+v", tr.Title, tr)
		}
		if tr.DiscNo != 1 {
			t.Errorf("track %q: DiscNo = %d, want 1", tr.Title, tr.DiscNo)
		}
	}
	if tracks[0].TrackNo != 1 || tracks[1].TrackNo != 2 {
		t.Errorf("track order/positions wrong: %+v", tracks)
	}
}

func TestClient_ErrorInHTTP200Body(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	_, err := c.GetTrack("999")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetTrack: err = %v, want *APIError", err)
	}
	if !apiErr.NotFound() {
		t.Errorf("NotFound() = false for code %d", apiErr.Code)
	}
}

func TestClient_EmptyDataWithPositiveTotalIsAnError(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	if _, err := c.SearchArtists("drake", 10); err == nil {
		t.Fatal("expected an error for data:[] with total>0, got nil")
	}
}

func TestClient_SearchAlbumsAndTop(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	albums, err := c.SearchAlbums("ram", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].RecordType != "album" || albums[0].NbTracks != 13 || albums[0].ArtistName != "Daft Punk" {
		t.Errorf("unexpected albums: %+v", albums)
	}

	top, err := c.GetArtistTop("27", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || len(top[0].Contributors) != 2 || top[0].Contributors[1].Name != "Julian Casablancas" || top[0].Contributors[1].ID != "1188" || top[0].CoverURL == "" {
		t.Errorf("unexpected top tracks: %+v", top)
	}
}

func TestClient_EnrichContributors(t *testing.T) {
	srv := mockDeezerServer(t)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	tracks := []Track{{ID: "3135553", Title: "One More Time"}}
	trackCalls = 0
	c.EnrichContributors(tracks)
	got := tracks[0].Contributors
	if len(got) != 2 || got[1].Name != "Romanthony" || got[1].ID != "555" {
		t.Fatalf("contributors = %+v (composers must be skipped)", got)
	}

	again := []Track{{ID: "3135553"}}
	c.EnrichContributors(again)
	if trackCalls != 1 || len(again[0].Contributors) != 2 {
		t.Errorf("second call should come from cache: calls=%d, %+v", trackCalls, again[0].Contributors)
	}
}

func TestImageURL_EmptyHashMeansNoImage(t *testing.T) {
	const real = "https://cdn-images.dzcdn.net/images/artist/638e69b9caaf9f9f3f8826febea7b543/1000x1000-000000-80-0-0.jpg"
	if got := imageURL("https://cdn-images.dzcdn.net/images/artist//1000x1000-000000-80-0-0.jpg"); got != "" {
		t.Errorf("empty hash: got %q, want no image", got)
	}
	if got := imageURL("", real); got != real {
		t.Errorf("fallback: got %q", got)
	}
}

// У плодовитых артистов больше 100 релизов: Deezer отдаёт их страницами,
// и старые синглы оказываются на второй — их тоже надо забрать.
func TestClient_GetArtistAlbums_WalksPages(t *testing.T) {
	const total = 109
	mux := http.NewServeMux()
	mux.HandleFunc("/artist/5/albums", func(w http.ResponseWriter, r *http.Request) {
		index, _ := strconv.Atoi(r.URL.Query().Get("index"))
		var data []map[string]any
		for i := index; i < total && i < index+100; i++ {
			data = append(data, map[string]any{"id": 1000 + i, "title": fmt.Sprintf("Release %d", i), "record_type": "single"})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "total": total})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClientWithBaseURL(srv.URL)

	albums, err := c.GetArtistAlbums("5", 500)
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(albums) != total {
		t.Fatalf("got %d albums, want %d", len(albums), total)
	}
	if last := albums[total-1]; last.ID != "1108" || last.ArtistID != "5" {
		t.Errorf("unexpected last album: %+v", last)
	}

	capped, err := c.GetArtistAlbums("5", 50)
	if err != nil || len(capped) != 50 {
		t.Fatalf("capped: got %d albums, err %v; want 50", len(capped), err)
	}
}
