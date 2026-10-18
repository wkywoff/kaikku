package ingest

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping DB-backed test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db ping: %v", err)
	}
	// LIFO: Close регистрируется первым, чтобы TRUNCATE ещё имел соединение.
	t.Cleanup(func() { db.Close() })
	t.Cleanup(func() {
		if _, err := db.Exec(`TRUNCATE ingest_jobs, tracks, albums, artists, users, track_edits RESTART IDENTITY CASCADE`); err != nil {
			t.Logf("cleanup truncate failed: %v", err)
		}
	})
	return db
}

func str(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func TestFindOrCreateArtist_SeparatesNamesakes(t *testing.T) {
	db := openTestDB(t)

	first, err := findOrCreateArtist(db, "415", "Nirvana")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := findOrCreateArtist(db, "415", "Nirvana")
	if again != first {
		t.Errorf("second lookup by deezer_id returned a different row")
	}
	other, err := findOrCreateArtist(db, "999001", "Nirvana") // 60-е, другая группа
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Errorf("a namesake with a different deezer_id must be a separate artist")
	}
	if _, err := findOrCreateArtist(db, "", "Nobody"); err == nil {
		t.Errorf("an artist without a deezer id must be refused")
	}
}

func compilationJob(sourceRef, title, artist, artistID string, discNo int32) *Job {
	return &Job{
		SourceRef:           sourceRef,
		Title:               title,
		ArtistName:          artist,
		ArtistDeezerID:      str(artistID),
		AlbumArtistName:     "Various Artists",
		AlbumArtistDeezerID: str("5080"),
		AlbumTitle:          "Compilation",
		AlbumDeezerID:       str("77"),
		TrackNo:             sql.NullInt32{Int32: 1, Valid: true},
		DiscNo:              sql.NullInt32{Int32: discNo, Valid: true},
		DurationMs:          180000,
		ReleaseDate:         str("2020-01-02"),
	}
}

func TestSaveTrack_CompilationIsOneAlbum(t *testing.T) {
	db := openTestDB(t)

	id1, created1, err := saveTrack(db, compilationJob("deezer:501", "One", "Artist A", "1", 1), "a.m4a", -9, -5)
	if err != nil || !created1 {
		t.Fatalf("first saveTrack: %v created=%v", err, created1)
	}
	id2, created2, err := saveTrack(db, compilationJob("deezer:502", "Two", "Artist B", "2", 2), "b.m4a", -9, -5)
	if err != nil || !created2 {
		t.Fatalf("second saveTrack: %v created=%v", err, created2)
	}

	var albums int
	db.QueryRow(`SELECT count(*) FROM albums`).Scan(&albums)
	if albums != 1 {
		t.Errorf("got %d albums, want 1 (tracks by different artists on one compilation)", albums)
	}
	var albumArtist, released string
	db.QueryRow(`SELECT ar.name, al.released_at::text FROM albums al JOIN artists ar ON ar.id = al.artist_id`).Scan(&albumArtist, &released)
	if albumArtist != "Various Artists" || released != "2020-01-02" {
		t.Errorf("album artist = %q, released = %q", albumArtist, released)
	}
	var a1, a2 string
	db.QueryRow(`SELECT ar.name FROM tracks t JOIN artists ar ON ar.id = t.artist_id WHERE t.id = $1`, id1).Scan(&a1)
	db.QueryRow(`SELECT ar.name FROM tracks t JOIN artists ar ON ar.id = t.artist_id WHERE t.id = $1`, id2).Scan(&a2)
	if a1 != "Artist A" || a2 != "Artist B" {
		t.Errorf("per-track artists = %q, %q", a1, a2)
	}
}

func TestSaveTrack_ExistingTrackWins(t *testing.T) {
	db := openTestDB(t)
	job := compilationJob("deezer:501", "One", "Artist A", "1", 1)

	first, _, err := saveTrack(db, job, "a.m4a", -9, -5)
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := saveTrack(db, job, "a-again.m4a", -9, -5)
	if err != nil {
		t.Fatal(err)
	}
	if created || second != first {
		t.Errorf("re-saving the same deezer track: created=%v id=%s, want existing %s", created, second, first)
	}
}

func TestFindOrCreateAlbum_KeepsManualEdits(t *testing.T) {
	db := openTestDB(t)
	artistID, _ := findOrCreateArtist(db, "1", "Artist")
	db.Exec(`UPDATE artists SET name = 'My Name', edited = true WHERE id = $1`, artistID)
	id, _ := findOrCreateAlbum(db, "77", artistID, "Deezer Title", str("2020-01-02"), str("album"))
	db.Exec(`UPDATE albums SET title = 'My Title', record_type = 'ep', edited = true WHERE id = $1`, id)

	findOrCreateArtist(db, "1", "Artist")
	findOrCreateAlbum(db, "77", artistID, "Deezer Title", str("2020-01-02"), str("album"))

	var name, title, typ string
	db.QueryRow(`SELECT name FROM artists WHERE id = $1`, artistID).Scan(&name)
	db.QueryRow(`SELECT title, record_type FROM albums WHERE id = $1`, id).Scan(&title, &typ)
	if name != "My Name" || title != "My Title" || typ != "ep" {
		t.Errorf("manual edits overwritten: %q %q %q", name, title, typ)
	}
}

// Админ поправил трек, пока его не было в медиатеке (track_edits), — при
// скачивании правка переходит в строку трека.
func TestSaveTrack_AppliesEditsMadeBeforeDownload(t *testing.T) {
	db := openTestDB(t)
	db.Exec(`INSERT INTO track_edits (deezer_id, title, track_no) VALUES ('501', 'Edited One', 7)`)
	id, created, err := saveTrack(db, compilationJob("deezer:501", "One", "Artist A", "1", 1), "a.m4a", -9, -5)
	if err != nil || !created {
		t.Fatal(err, created)
	}
	var title string
	var trackNo int
	var edited bool
	db.QueryRow(`SELECT title, track_no, edited FROM tracks WHERE id = $1`, id).Scan(&title, &trackNo, &edited)
	if title != "Edited One" || trackNo != 7 || !edited {
		t.Errorf("track row: %q #%d edited=%v", title, trackNo, edited)
	}
	var left int
	db.QueryRow(`SELECT count(*) FROM track_edits`).Scan(&left)
	if left != 0 {
		t.Errorf("the edit should move into the track row, %d left in track_edits", left)
	}
}
