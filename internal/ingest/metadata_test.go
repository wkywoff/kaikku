package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"kaikku/internal/deezer"
)

func TestFillMetadata_DoesNotFetchArtistPictures(t *testing.T) {
	db := openTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("Deezer asked for %s", r.URL.Path)
	}))
	defer srv.Close()
	db.Exec(`INSERT INTO artists (name, deezer_id) VALUES ('Faceless', '7')`)

	FillMetadata(context.Background(), db, deezer.NewClientWithBaseURL(srv.URL))
}

func TestFillMetadata_SkipsOwnUploads(t *testing.T) {
	db := openTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("Deezer asked for %s", r.URL.Path)
	}))
	defer srv.Close()
	dz := deezer.NewClientWithBaseURL(srv.URL)

	var artistID, albumID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('Own', 'k1') RETURNING id`).Scan(&artistID)
	db.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'Own EP', 'k2') RETURNING id`, artistID).Scan(&albumID)
	if _, err := db.Exec(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, deezer_id)
		VALUES ($1, 'Own song', 1000, 'uploads/x.m4a', 'k3')`, albumID); err != nil {
		t.Fatal(err)
	}

	FillMetadata(context.Background(), db, dz)
}
