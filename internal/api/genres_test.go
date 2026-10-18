package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"kaikku/internal/deezer"
)

func TestGenres(t *testing.T) {
	db, router, token, _ := newTestServer(t)
	db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`) })
	me := userIDOf(t, router, token)
	db.Exec(`INSERT INTO artist_tags (deezer_id, name, tags) VALUES
	         ('1', 'Test Artist', '[{"name":"phonk","count":100},{"name":"rap","count":40}]'),
	         ('2', 'Other Artist', '[{"name":"phonk","count":70}]'),
	         ('3', 'Feat Artist', '[{"name":"rap","count":90}]')`)
	meta := `{"id":"700","title":"Mine","artist":"Other Artist","artist_id":"2","album":"Al","album_id":"9"}`
	for range 3 {
		db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id) VALUES ($1, '700', $2, 'album', '9')`, me, meta)
	}

	var list struct{ Genres []genre }
	json.Unmarshal(call(t, router, "GET", "/api/genres", token, "", http.StatusOK), &list)
	if len(list.Genres) < 2 || list.Genres[0].Slug != "phonk" {
		t.Fatalf("genres: %+v (your genre first, then the popular ones)", list.Genres)
	}

	var page struct {
		Genre   genre
		Own     []deezer.Track
		Artists []deezer.Artist
		Tracks  []deezer.Track
	}
	json.Unmarshal(call(t, router, "GET", "/api/genres/phonk", token, "", http.StatusOK), &page)
	ids := func(ts []deezer.Track) string {
		s := ""
		for _, t := range ts {
			s += t.ID + ","
		}
		return s
	}
	if page.Genre.Name != "phonk" || ids(page.Own) != "700," || len(page.Artists) != 1 || page.Artists[0].ID != "1" || ids(page.Tracks) != "888," {
		t.Fatalf("phonk: %+v own %s tracks %s", page, ids(page.Own), ids(page.Tracks))
	}
	call(t, router, "GET", "/api/genres/jazz", token, "", http.StatusNotFound)
}
