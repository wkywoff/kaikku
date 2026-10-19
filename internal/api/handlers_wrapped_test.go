package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// addPlay records a play like POST /api/me/plays does, at a given moment.
func addPlay(t *testing.T, db *sql.DB, token string, router http.Handler, user, track, artistID, album string, sec int, at time.Time) {
	t.Helper()
	meta := fmt.Sprintf(`{"id":%q,"title":"Song %s","artist":"Artist %s","artist_id":%q,"album":"Album %s","album_id":%q,"cover_url":"http://example.com/%s.jpg","duration_ms":%d}`,
		track, track, artistID, artistID, album, album, album, sec*1000)
	if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, played_at) VALUES ($1, $2, $3, 'album', $4, $5)`,
		user, track, meta, album, at); err != nil {
		t.Fatal(err)
	}
}

func userIDOf(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	var me struct{ ID string }
	json.Unmarshal(call(t, router, "GET", "/api/me", token, "", http.StatusOK), &me)
	return me.ID
}

func TestWrapped(t *testing.T) {
	db, router, token := newTestRouter(t)
	db.Exec(`TRUNCATE plays`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays`) })
	other := invitedUser(t, router, token, "wrapped-other")
	me, them := userIDOf(t, router, token), userIDOf(t, router, other)

	msk, _ := time.LoadLocation("Europe/Moscow")
	at := func(month time.Month, day, hour int) time.Time {
		return time.Date(2025, month, day, hour, 30, 0, 0, msk)
	}
	// 2025, по Москве: три дня подряд в марте, день в июле; «ночью» в 1:30.
	addPlay(t, db, token, router, me, "100", "71", "500", 180, at(3, 1, 1)) // первый трек года
	addPlay(t, db, token, router, me, "101", "72", "501", 240, at(3, 2, 20))
	addPlay(t, db, token, router, me, "101", "72", "501", 240, at(3, 3, 20))
	addPlay(t, db, token, router, me, "101", "72", "501", 240, at(7, 10, 20))
	addPlay(t, db, token, router, me, "102", "72", "502", 120, at(7, 10, 21))
	// Не в 2025 по Москве: 31 декабря 2024, 23:30 по Москве — это ещё 2024.
	addPlay(t, db, token, router, me, "103", "73", "503", 60, time.Date(2024, 12, 31, 23, 30, 0, 0, msk))
	// Другой человек слушал исполнителя 72 больше (5 раз) — у меня второе место.
	for i := 0; i < 5; i++ {
		addPlay(t, db, other, router, them, "101", "72", "501", 240, at(5, 1+i, 12))
	}

	var w wrappedStats
	json.Unmarshal(call(t, router, "GET", "/api/me/wrapped?year=2025&tz=Europe/Moscow", token, "", http.StatusOK), &w)
	if w.Plays != 5 || w.Minutes != (180+240*3+120)/60 || w.Tracks != 3 || w.Artists != 2 {
		t.Fatalf("totals: %+v", w)
	}
	if len(w.TopTracks) != 3 || w.TopTracks[0].ID != "101" || w.TopTracks[0].Plays != 3 || w.TopTracks[0].Title != "Song 101" {
		t.Errorf("top tracks: %+v", w.TopTracks)
	}
	if len(w.TopArtists) != 2 || w.TopArtists[0].ID != "72" || w.TopArtists[0].Plays != 4 || w.TopArtists[0].Minutes != 14 {
		t.Errorf("top artists: %+v", w.TopArtists)
	}
	if len(w.TopAlbums) != 3 || w.TopAlbums[0].ID != "501" || w.TopAlbums[0].CoverURL != "http://example.com/501.jpg" {
		t.Errorf("top albums: %+v", w.TopAlbums)
	}
	if w.Months[2] != (180+480)/60 || w.Months[6] != 6 || w.Hours[1] != 1 || w.Hours[20] != 3 {
		t.Errorf("months %v, hours %v", w.Months, w.Hours)
	}
	if w.Streak != 3 || w.TopDay == nil || w.TopDay.Date != "2025-07-10" || w.TopDay.Minutes != 6 {
		t.Errorf("streak %d, top day %+v", w.Streak, w.TopDay)
	}
	if w.First == nil || w.First.ID != "100" || w.Since != "2025-03-01" {
		t.Errorf("first: %+v since %s", w.First, w.Since)
	}
	if w.Listeners == nil || w.Listeners.Rank != 2 || w.Listeners.Listeners != 2 {
		t.Errorf("listeners: %+v", w.Listeners)
	}
	if len(w.Years) != 2 || w.Years[0] != 2024 || w.Years[1] != 2025 {
		t.Errorf("years: %v", w.Years)
	}

	// Год без прослушиваний — пустые итоги, а не ошибка; чужой год не виден.
	var empty wrappedStats
	json.Unmarshal(call(t, router, "GET", "/api/me/wrapped?year=2023", token, "", http.StatusOK), &empty)
	if empty.Plays != 0 || len(empty.TopTracks) != 0 {
		t.Errorf("empty year: %+v", empty)
	}
	call(t, router, "GET", "/api/me/wrapped?year=1999", token, "", http.StatusBadRequest)
	call(t, router, "GET", "/api/me/wrapped?year=abc", token, "", http.StatusBadRequest)
}

// Одна запись на разных релизах — одна песня (по ISRC, а в истории без него —
// по исполнителю, названию и длине); исполнители и альбомы — с правками админа.
func TestWrappedMergesReleasesAndUsesEdits(t *testing.T) {
	db, router, token := newTestRouter(t)
	db.Exec(`TRUNCATE plays`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays`) })
	me := userIDOf(t, router, token)
	play := func(track, isrc, title, album string, sec int, day int) {
		t.Helper()
		meta := fmt.Sprintf(`{"id":%q,"title":%q,"artist":"Deezer Name","artist_id":"81","album":"Deezer Album %s","album_id":%q,"duration_ms":%d,"isrc":%q}`,
			track, title, album, album, sec*1000, isrc)
		if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, played_at) VALUES ($1, $2, $3, 'album', $4, $5)`,
			me, track, meta, album, time.Date(2025, 4, day, 12, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	play("201", "ISRC1", "Same Song", "601", 200, 1) // альбом
	play("201", "ISRC1", "Same Song", "601", 200, 2)
	play("202", "ISRC1", "Same Song", "602", 200, 3) // делюкс
	play("301", "", "Old Song", "603", 180, 4)       // история без ISRC: сингл
	play("302", "", "old song ", "604", 181, 5)      // …и альбом
	play("302", "", "old song ", "604", 181, 6)
	play("303", "", "Old Song", "605", 260, 7) // другая запись: длиннее

	var artistID string
	db.QueryRow(`INSERT INTO artists (name, deezer_id, edited) VALUES ('Edited Artist', '81', true) RETURNING id`).Scan(&artistID)
	db.Exec(`INSERT INTO albums (artist_id, title, deezer_id, edited) VALUES ($1, 'Edited Album', '601', true)`, artistID)

	var w wrappedStats
	json.Unmarshal(call(t, router, "GET", "/api/me/wrapped?year=2025", token, "", http.StatusOK), &w)
	if w.Tracks != 3 || len(w.TopTracks) != 3 {
		t.Fatalf("want 3 songs, got %d: %+v", w.Tracks, w.TopTracks)
	}
	got := map[string]int{}
	for _, tr := range w.TopTracks {
		got[tr.ID] = tr.Plays
	}
	if got["201"] != 3 || got["302"] != 3 || got["303"] != 1 {
		t.Errorf("merged plays: %v", got)
	}
	if len(w.TopArtists) != 1 || w.TopArtists[0].Name != "Edited Artist" {
		t.Errorf("artist edits: %+v", w.TopArtists)
	}
	found := false
	for _, a := range w.TopAlbums {
		if a.ID == "601" {
			found = a.Title == "Edited Album" && a.Artist == "Edited Artist"
		}
	}
	if !found {
		t.Errorf("album edits: %+v", w.TopAlbums)
	}
}

// Минуты — сколько трек реально играл (плеер присылает), у старых
// прослушиваний без этого — длина трека.
func TestWrappedCountsListenedTime(t *testing.T) {
	db, router, token := newTestRouter(t)
	db.Exec(`TRUNCATE plays`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays`) })
	other := invitedUser(t, router, token, "listened-other")
	me := userIDOf(t, router, token)

	at := time.Date(2025, 4, 1, 12, 0, 0, 0, time.UTC)
	addPlay(t, db, token, router, me, "110", "81", "510", 240, at)
	addPlay(t, db, token, router, me, "111", "81", "510", 240, at.Add(time.Hour)) // старое: без listened_ms
	var id int64
	db.QueryRow(`SELECT id FROM plays WHERE deezer_id = '110'`).Scan(&id)
	listened := func(who string, ms int, want int) {
		t.Helper()
		call(t, router, "POST", fmt.Sprintf("/api/me/plays/%d/listened", id), who, fmt.Sprintf(`{"ms":%d}`, ms), want)
	}
	stored := func() int {
		var ms int
		db.QueryRow(`SELECT listened_ms FROM plays WHERE id = $1`, id).Scan(&ms)
		return ms
	}

	listened(token, 30000, http.StatusNoContent)
	listened(token, 20000, http.StatusNoContent) // запоздавшее меньшее не уменьшает
	if got := stored(); got != 30000 {
		t.Errorf("listened_ms = %d, want 30000", got)
	}
	listened(other, 90000, http.StatusNotFound) // чужое прослушивание
	listened(token, 90000, http.StatusNoContent)
	listened(token, -1, http.StatusBadRequest)

	var w wrappedStats
	json.Unmarshal(call(t, router, "GET", "/api/me/wrapped?year=2025&tz=UTC", token, "", http.StatusOK), &w)
	if w.Plays != 2 || w.Minutes != (90+240)/60 {
		t.Errorf("plays %d, minutes %d; want 2 plays, %d minutes", w.Plays, w.Minutes, (90+240)/60)
	}

	listened(token, 999999, http.StatusNoContent) // не длиннее трека
	if got := stored(); got != 240000 {
		t.Errorf("listened_ms = %d, want the song length 240000", got)
	}
}
