package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"kaikku/internal/deezer"
)

func TestTagSlug(t *testing.T) {
	for in, want := range map[string]string{"russian hip-hop": "russian-hip-hop", "r&b": "r-b", "  lo-fi  ": "lo-fi", "80s": "80s"} {
		if got := tagSlug(in); got != want {
			t.Errorf("tagSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func tt(id, artistID string, plays, plays30, plays90 int, last, liked time.Time) tasteTrack {
	return tasteTrack{
		Track: deezer.Track{ID: id, Title: "T" + id, ArtistID: artistID, Artist: "A" + artistID, CoverURL: "c" + id},
		plays: plays, plays30: plays30, plays90: plays90, last: last, liked: liked,
	}
}

func TestDistinctCovers(t *testing.T) {
	plans := []mixPlan{{mix: mix{Covers: []string{"a", "b"}}}, {mix: mix{Covers: []string{"a", "c"}}}, {mix: mix{Covers: []string{"a"}}}}
	distinctCovers(plans)
	if plans[0].Covers[0] != "a" || plans[1].Covers[0] != "c" || plans[2].Covers[0] != "a" {
		t.Fatalf("covers: %v", plans)
	}
}

func TestOnRepeatAndForgotten(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -100)
	var taste []tasteTrack
	for i := range 6 {
		taste = append(taste, tt(fmt.Sprint(10+i), "1", 2+i, 2+i, 2+i, now, time.Time{}))
	}
	taste = append(taste, tt("99", "2", 1, 1, 1, now, time.Time{})) // разок — не «на повторе»
	p, ok := onRepeatMix(taste, now)
	if !ok || len(p.tracks) != 6 || p.tracks[0].ID != "15" || p.Artists[0] != "A1" {
		t.Fatalf("on repeat: %v %+v", ok, p)
	}
	if _, ok := forgottenMix(taste, now, dailyRand("u", "forgotten", now)); ok {
		t.Fatal("nothing is forgotten yet")
	}
	for i := range 5 {
		taste = append(taste, tt(fmt.Sprint(50+i), "3", 0, 0, 0, time.Time{}, old)) // лайкнул давно, не слушал
	}
	taste = append(taste, tt("60", "3", 0, 0, 0, time.Time{}, now)) // лайкнул вчера — не забытое
	p, ok = forgottenMix(taste, now, dailyRand("u", "forgotten", now))
	if !ok || len(p.tracks) != 5 || slices.ContainsFunc(p.tracks, func(x deezer.Track) bool { return x.ID == "60" }) {
		t.Fatalf("forgotten: %v %+v", ok, p.tracks)
	}
	again, _ := forgottenMix(taste, now, dailyRand("u", "forgotten", now))
	if fmt.Sprint(again.tracks) != fmt.Sprint(p.tracks) {
		t.Fatal("the order is the same all day")
	}
}

// Жанр — где у человека хотя бы два артиста; почти тот же состав — повтор.
func TestTagMixes(t *testing.T) {
	now := time.Now()
	taste := []tasteTrack{tt("1", "1", 9, 9, 9, now, time.Time{}), tt("2", "2", 5, 5, 5, now, time.Time{}), tt("3", "3", 1, 1, 1, now, now)}
	w, names := artistWeights(taste)
	if w["3"] != 1+mixLikeWeight {
		t.Fatalf("weights: %v", w)
	}
	tags := map[string][]artistTag{
		"1": {{"phonk", 100}, {"rap", 90}, {"jazz", 100}},
		"2": {{"phonk", 80}, {"rap", 100}},
		"3": {{"rap", 50}},
	}
	plans := tagMixes(taste, w, names, topArtists(w, 30), tags)
	if len(plans) != 1 || plans[0].ID != "tag-rap" || strings.Join(plans[0].artists, ",") != "1,2,3" || plans[0].Artists[0] != "A1" {
		t.Fatalf("plans: %+v", plans)
	}
}

func TestMixes_API(t *testing.T) {
	db, router, token, _ := newTestServer(t)
	db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`) })
	me := userIDOf(t, router, token)
	play := func(id, artistID string, n int) {
		t.Helper()
		meta := fmt.Sprintf(`{"id":%q,"title":"Song %s","artist":"Artist %s","artist_id":%q,"album":"Al","album_id":"9","cover_url":"http://x/%s.jpg","duration_ms":1000}`,
			id, id, artistID, artistID, id)
		for range n {
			if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, played_at) VALUES ($1, $2, $3, 'album', '9', now() - interval '1 day')`, me, id, meta); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range 4 {
		play(fmt.Sprint(700+i), "1", 3)
	}
	play("800", "2", 2)
	db.Exec(`INSERT INTO artist_tags (deezer_id, tags) VALUES ('1', '[{"name":"phonk","count":100}]'), ('2', '[{"name":"phonk","count":60}]')`)

	var list struct{ Mixes []mix }
	json.Unmarshal(call(t, router, "GET", "/api/me/mixes", token, "", http.StatusOK), &list)
	ids := []string{}
	for _, m := range list.Mixes {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "on-repeat,tag-phonk" || list.Mixes[1].Tag != "phonk" || len(list.Mixes[1].Covers) != 4 {
		t.Fatalf("mixes: %+v", list.Mixes)
	}

	// Жанровый: по кругу от каждого артиста — его треки у человека, потом
	// хиты Deezer (у артиста 1 — трек 888).
	var got struct {
		Mix    mix
		Tracks []deezer.Track
	}
	json.Unmarshal(call(t, router, "GET", "/api/me/mixes/tag-phonk", token, "", http.StatusOK), &got)
	var order []string
	for _, tr := range got.Tracks {
		order = append(order, tr.ID)
	}
	if got.Mix.Kind != "tag" || strings.Join(order, ",") != "700,800,701,702,888" {
		t.Fatalf("tag mix: %v", order)
	}
	call(t, router, "GET", "/api/me/mixes/tag-jazz", token, "", http.StatusNotFound)
	call(t, router, "GET", "/api/me/mixes/Tag-Phonk", token, "", http.StatusNotFound)

	// Включённое из микса — в «Недавно прослушанном» карточкой микса.
	call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"888","context":{"kind":"mix","id":"tag-phonk"}}`, http.StatusNoContent)
	var recent struct{ Items []recentItem }
	json.Unmarshal(call(t, router, "GET", "/api/me/recent", token, "", http.StatusOK), &recent)
	if len(recent.Items) == 0 || recent.Items[0].Kind != "mix" || recent.Items[0].Mix.Tag != "phonk" {
		t.Fatalf("recent: %+v", recent.Items)
	}
	// Несуществующий микс — как трек альбома.
	call(t, router, "POST", "/api/me/plays", token, `{"deezer_id":"888","context":{"kind":"mix","id":"tag-jazz"}}`, http.StatusNoContent)
	var kind string
	db.QueryRow(`SELECT context_kind FROM plays ORDER BY id DESC LIMIT 1`).Scan(&kind)
	if kind != "album" {
		t.Fatalf("unknown mix context: %s", kind)
	}
}

// План миксов кешируется на минуту, но лайк его сбрасывает.
func TestMixes_CachedUntilLike(t *testing.T) {
	db, router, token, _ := newTestServer(t)
	db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`)
	t.Cleanup(func() { db.Exec(`TRUNCATE plays, liked_tracks, artist_tags`) })
	me := userIDOf(t, router, token)
	play := func(id string) {
		t.Helper()
		meta := fmt.Sprintf(`{"id":%q,"title":"Song %s","artist":"A","artist_id":"1","album":"Al","album_id":"9"}`, id, id)
		for range 2 {
			if _, err := db.Exec(`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id) VALUES ($1, $2, $3, 'album', '9')`, me, id, meta); err != nil {
				t.Fatal(err)
			}
		}
	}
	mixIDs := func() string {
		var list struct{ Mixes []mix }
		json.Unmarshal(call(t, router, "GET", "/api/me/mixes", token, "", http.StatusOK), &list)
		var ids []string
		for _, m := range list.Mixes {
			ids = append(ids, m.ID)
		}
		return strings.Join(ids, ",")
	}
	for _, id := range []string{"901", "902", "903", "904"} {
		play(id)
	}
	if got := mixIDs(); got != "" {
		t.Fatalf("4 tracks on repeat are not a mix yet: %q", got)
	}
	play("905")
	if got := mixIDs(); got != "" {
		t.Fatalf("within a minute the plan comes from the cache: %q", got)
	}
	call(t, router, "PUT", "/api/me/liked/888", token, "", http.StatusNoContent)
	if got := mixIDs(); got != "on-repeat" {
		t.Fatalf("a like recomputes the mixes: %q", got)
	}
}

// Совместный трек в топе у двух артистов попадает в список один раз:
// повтор ронял страницу жанра.
func TestInterleaveSkipsRepeats(t *testing.T) {
	tr := func(id string) deezer.Track { return deezer.Track{ID: id} }
	got := interleave([][]deezer.Track{{tr("1"), tr("shared"), tr("2")}, {tr("shared"), tr("3")}}, 10)
	var ids []string
	for _, x := range got {
		ids = append(ids, x.ID)
	}
	if strings.Join(ids, ",") != "1,shared,3,2" {
		t.Errorf("interleave = %v", ids)
	}
}
