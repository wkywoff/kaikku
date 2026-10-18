package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"kaikku/internal/lastfm"
	"kaikku/internal/safe"
)

// Теги артистов с Last.fm (миграция 0025): жанры на странице артиста и
// основа миксов (mixes.go). Ищутся по имени, как описание (bio.go). Фоновый
// проход раз в 15 минут подтягивает теги всех, кого слушают, лайкают и на
// кого подписаны; страница артиста спрашивает сама, если тегов ещё нет.
// Владелец своего артиста и админ ставят жанры сами (custom, миграция
// 0034): их Last.fm уже не перезаписывает.

const (
	// Сроки — в запросе tagListenedArtists: 30 дней, «тегов нет» — 7.
	tagsPerArtist = 6
	// Ниже этого веса тег — случайный (поставили пару человек).
	tagMinCount = 10
	tagsEvery   = 15 * time.Minute
	tagsBatch   = 40
	tagsPasses  = 10
	tagsPause   = 250 * time.Millisecond // Last.fm просит не больше 5 запросов в секунду
)

type artistTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Теги, которые ничего не говорят о музыке: оценки, «видел вживую», страна.
var junkTags = map[string]bool{
	"seen live": true, "favorites": true, "favourites": true, "favorite": true, "favourite": true,
	"my favorite": true, "love": true, "awesome": true, "beautiful": true, "cool": true, "amazing": true,
	"albums i own": true, "spotify": true, "under 2000 listeners": true, "all": true, "music": true,
	"female vocalists": true, "male vocalists": true, "female vocalist": true, "male vocalist": true,
	"russian": true, "russia": true, "ukrainian": true, "ukraine": true, "belarusian": true, "kazakh": true,
	"estonian": true, "american": true, "usa": true, "british": true, "uk": true, "english": true,
	"german": true, "germany": true, "french": true, "france": true, "japanese": true, "japan": true,
	"korean": true, "swedish": true, "canadian": true, "polish": true, "italian": true, "spanish": true,
	"australian": true, "brazilian": true, "finnish": true, "norwegian": true, "dutch": true,
}

var tagSpaces = regexp.MustCompile(`[\s_]+`)

// normalizeTag: «Hip Hop», «hiphop» и «hip-hop» — один тег.
func normalizeTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(tagSpaces.ReplaceAllString(s, " ")))
	switch s {
	case "hip hop", "hiphop":
		return "hip-hop"
	case "rnb", "r and b", "r'n'b":
		return "r&b"
	}
	return s
}

// cleanTags — теги артиста без мусора и повторов, самые весомые сначала.
func cleanTags(raw []lastfm.Tag, artist string) []artistTag {
	name := normalizeTag(artist)
	seen := map[string]bool{}
	out := []artistTag{}
	for _, t := range raw {
		n := normalizeTag(t.Name)
		if t.Count < tagMinCount || n == "" || len(n) > 40 || junkTags[n] || seen[n] || (name != "" && strings.Contains(n, name)) {
			continue
		}
		seen[n] = true
		out = append(out, artistTag{Name: n, Count: t.Count})
		if len(out) == tagsPerArtist {
			break
		}
	}
	return out
}

// tagsOf — сохранённые теги артистов (кого нет — того нет в ответе).
func (s *Server) tagsOf(ids []string) (map[string][]artistTag, error) {
	out := map[string][]artistTag{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(`SELECT deezer_id, tags FROM artist_tags WHERE deezer_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		var tags []artistTag
		if rows.Scan(&id, &raw) == nil && json.Unmarshal(raw, &tags) == nil {
			out[id] = tags
		}
	}
	return out, rows.Err()
}

// fetchTags спрашивает Last.fm и запоминает ответ (пустой — тоже).
func (s *Server) fetchTags(ctx context.Context, id, name string) ([]artistTag, error) {
	raw, err := s.Lastfm.ArtistTopTags(ctx, name)
	if err != nil {
		return nil, err
	}
	tags := cleanTags(raw, name)
	js, _ := json.Marshal(tags)
	if _, err := s.DB.Exec(
		`INSERT INTO artist_tags (deezer_id, tags, name) VALUES ($1, $2, $3)
		 ON CONFLICT (deezer_id) DO UPDATE SET tags = EXCLUDED.tags, name = EXCLUDED.name, updated_at = now()
		 WHERE NOT artist_tags.custom`, id, js, name); err != nil {
		log.Printf("tags of %s: save: %v", id, err)
	}
	return tags, nil
}

// artistTags — теги для страницы артиста: сохранённые, а если их ещё нет —
// сразу с Last.fm. Свои артисты (k…) на Last.fm не ищутся. custom — жанры
// поставлены вручную.
func (s *Server) artistTags(ctx context.Context, id string) (tags []artistTag, custom bool) {
	var raw []byte
	err := s.DB.QueryRow(`SELECT tags, custom FROM artist_tags WHERE deezer_id = $1`, id).Scan(&raw, &custom)
	if err == nil && json.Unmarshal(raw, &tags) == nil {
		return tags, custom // устаревшие обновит фоновый проход
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("tags of %s: %v", id, err)
	}
	if s.Lastfm == nil || !validDeezerID(id) {
		return []artistTag{}, false
	}
	a, err := s.catalogArtist(id)
	if err != nil {
		return []artistTag{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tags, err = s.fetchTags(ctx, id, a.Name)
	if err != nil {
		log.Printf("tags of %s: last.fm: %v", id, err)
		return []artistTag{}, false
	}
	return tags, false
}

// tagNames — названия тегов для страницы артиста.
func tagNames(tags []artistTag) []string {
	out := []string{}
	for _, t := range tags {
		if len(out) < artistPageTags {
			out = append(out, t.Name)
		}
	}
	return out
}

// PUT /api/discover/artists/{id}/tags {"tags": [...]} — свои жанры (права —
// как у описания, bio.go). Порядок — важность: первый весит больше всех.
// Пустой список — жанров нет.
func (s *Server) handlePutArtistTags(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "tags are required")
		return
	}
	tags := []artistTag{}
	seen := map[string]bool{}
	for _, raw := range req.Tags {
		n := normalizeTag(raw)
		if n == "" || seen[n] {
			continue
		}
		if utf8.RuneCountInString(n) > 40 {
			writeError(w, http.StatusBadRequest, "a genre is up to 40 characters")
			return
		}
		seen[n] = true
		tags = append(tags, artistTag{Name: n, Count: max(100-20*len(tags), 20)})
	}
	if len(tags) > artistPageTags {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("up to %d genres", artistPageTags))
		return
	}
	var name sql.NullString
	if a, err := s.catalogArtist(id); err == nil && a.Name != "" {
		name = sql.NullString{String: a.Name, Valid: true}
	}
	js, _ := json.Marshal(tags)
	if _, err := s.DB.Exec(
		`INSERT INTO artist_tags (deezer_id, tags, name, custom) VALUES ($1, $2, $3, true)
		 ON CONFLICT (deezer_id) DO UPDATE SET tags = EXCLUDED.tags, name = COALESCE(EXCLUDED.name, artist_tags.name),
		        custom = true, updated_at = now()`, id, js, name); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	s.forgetGenres()
	writeJSON(w, http.StatusOK, map[string]any{"tags": tagNames(tags), "tags_custom": true})
}

// DELETE /api/discover/artists/{id}/tags — снова жанры с Last.fm.
func (s *Server) handleDeleteArtistTags(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM artist_tags WHERE deezer_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reset")
		return
	}
	s.forgetGenres()
	tags, _ := s.artistTags(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tagNames(tags), "tags_custom": false})
}

// forgetGenres — жанры артиста поменялись: плитки и страницы жанров заново.
func (s *Server) forgetGenres() {
	s.cache.forget("genres:")
	s.cache.forget("genre:")
}

// StartTagger — фоновый проход по артистам, которых слушают: теги для миксов.
func (s *Server) StartTagger(ctx context.Context) {
	if s.Lastfm == nil {
		return
	}
	go func() {
		t := time.NewTimer(30 * time.Second) // не мешать старту
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			safe.Run("tagger", func() { s.tagListenedArtists(ctx) })
			t.Reset(tagsEvery)
		}
	}()
}

// tagListenedArtists подтягивает теги артистов без них (или с устаревшими):
// из истории за полгода, лайков и подписок — пачками по tagsBatch, за один
// проход не больше tagsPasses пачек (кого Last.fm не отдал — снова в
// следующей пачке, так что бесконечно не крутимся).
func (s *Server) tagListenedArtists(ctx context.Context) {
	for range tagsPasses {
		if full := s.tagBatch(ctx); !full {
			return
		}
	}
}

// tagBatch — одна пачка; true — пачка была полной, может быть, есть ещё.
// Имя — из снимка, то есть как у Deezer: под ним артиста знает и Last.fm.
func (s *Server) tagBatch(ctx context.Context) bool {
	rows, err := s.DB.Query(`
		SELECT id, max(name) FROM (
			SELECT meta->>'artist_id' AS id, meta->>'artist' AS name FROM plays WHERE played_at > now() - interval '180 days'
			UNION ALL SELECT meta->>'artist_id', meta->>'artist' FROM liked_tracks
			UNION ALL SELECT deezer_id, meta->>'name' FROM followed_artists) x
		 WHERE id ~ '^[1-9][0-9]*$' AND COALESCE(name, '') <> ''
		   AND NOT EXISTS (
		       SELECT 1 FROM artist_tags t WHERE t.deezer_id = x.id
		          AND (t.custom OR t.updated_at > now() - CASE WHEN t.tags = '[]' THEN interval '7 days' ELSE interval '30 days' END))
		 GROUP BY id
		 LIMIT $1`, tagsBatch)
	if err != nil {
		log.Printf("tagger: %v", err)
		return false
	}
	type artist struct{ id, name string }
	var todo []artist
	for rows.Next() {
		var a artist
		if rows.Scan(&a.id, &a.name) == nil {
			todo = append(todo, a)
		}
	}
	rows.Close()
	for _, a := range todo {
		if ctx.Err() != nil {
			return false
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.fetchTags(cctx, a.id, a.name)
		cancel()
		if lastfm.Retryable(err) {
			log.Printf("tagger: last.fm: %v (next pass later)", err)
			return false
		}
		if err != nil {
			log.Printf("tagger: %s (%s): %v", a.id, a.name, err)
		}
		time.Sleep(tagsPause)
	}
	return len(todo) == tagsBatch
}
