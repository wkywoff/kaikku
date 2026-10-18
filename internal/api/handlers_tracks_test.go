package api

import (
	"os"
	"path/filepath"
	"testing"
)

// Одна запись на двух релизах делит файл (ISRC, worker.go): удаление одного
// трека его не трогает, удаление последнего — удаляет.
func TestDeleteTrackKeepsSharedFile(t *testing.T) {
	srv, router := newAuthTestServer(t)
	admin := tokenOf(t, register(router, "owner", ""))

	var artistID, albumID, first, second string
	srv.DB.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('A', '1') RETURNING id`).Scan(&artistID)
	srv.DB.QueryRow(`INSERT INTO albums (artist_id, title, deezer_id) VALUES ($1, 'B', '2') RETURNING id`, artistID).Scan(&albumID)
	for i, dz := range []string{"3", "4"} {
		var id string
		srv.DB.QueryRow(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, deezer_id, isrc)
		                 VALUES ($1, 'T', 1000, 'shared.m4a', $2, 'ISRC') RETURNING id`, albumID, dz).Scan(&id)
		if i == 0 {
			first = id
		} else {
			second = id
		}
	}
	file := filepath.Join(srv.StorageDir, "shared.m4a")
	os.WriteFile(file, []byte("x"), 0o644)

	if rec := request(router, "DELETE", "/api/tracks/"+first, admin); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file is still used by the other release: %v", err)
	}
	if rec := request(router, "DELETE", "/api/tracks/"+second, admin); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the last track's file should be deleted: %v", err)
	}
}
