package ingest

import (
	"database/sql"
	"errors"
	"fmt"
)

// Каталог идентифицируется по Deezer ID (миграции 0002, 0012): два разных
// исполнителя с одинаковым именем — разные строки, а альбом всегда
// принадлежит своему альбомному исполнителю. Воркер обрабатывает несколько
// job'ов параллельно — вставки защищены ON CONFLICT (deezer_id).
//
// Поля, которые админ поправил руками (edited, миграция 0006), из Deezer
// больше не перезаписываются.

var errNoDeezerID = errors.New("catalog row needs a deezer id")

func findOrCreateArtist(db *sql.DB, deezerID, name string) (string, error) {
	if deezerID == "" {
		return "", fmt.Errorf("artist %q: %w", name, errNoDeezerID)
	}
	var id string
	err := db.QueryRow(
		`INSERT INTO artists (name, deezer_id) VALUES ($1, $2)
		 ON CONFLICT (deezer_id) DO UPDATE SET name = CASE WHEN artists.edited THEN artists.name ELSE EXCLUDED.name END
		 RETURNING id`, name, deezerID,
	).Scan(&id)
	return id, err
}

func findOrCreateAlbum(db *sql.DB, deezerID, artistID, title string, releaseDate, recordType sql.NullString) (string, error) {
	if deezerID == "" {
		return "", fmt.Errorf("album %q: %w", title, errNoDeezerID)
	}
	var id string
	err := db.QueryRow(
		`INSERT INTO albums (artist_id, title, released_at, deezer_id, record_type) VALUES ($1, $2, $3::date, $4, $5)
		 ON CONFLICT (deezer_id) DO UPDATE SET
		        artist_id   = EXCLUDED.artist_id,
		        title       = CASE WHEN albums.edited THEN albums.title ELSE EXCLUDED.title END,
		        released_at = CASE WHEN albums.edited THEN albums.released_at ELSE COALESCE(EXCLUDED.released_at, albums.released_at) END,
		        record_type = CASE WHEN albums.edited THEN albums.record_type ELSE COALESCE(EXCLUDED.record_type, albums.record_type) END
		 RETURNING id`,
		artistID, title, releaseDate, deezerID, recordType,
	).Scan(&id)
	return id, err
}

// EnsureArtist and EnsureAlbum let an admin edit an artist or album that
// has nothing downloaded yet (api/handlers_edit.go, "claim"): its catalog
// row is created from Deezer up front, and the worker later adds tracks to
// the same row — the edited fields stay as the admin set them.
func EnsureArtist(db *sql.DB, deezerID, name string) (string, error) {
	return findOrCreateArtist(db, deezerID, name)
}

func EnsureAlbum(db *sql.DB, deezerID, artistID, title string, releaseDate, recordType sql.NullString) (string, error) {
	return findOrCreateAlbum(db, deezerID, artistID, title, releaseDate, recordType)
}
