package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// Миксы — плейлисты, которые собираются сами из истории и лайков человека
// (своя база, без Deezer: персональные подборки Deezer без входа в его
// аккаунт недоступны):
//
//   on-repeat   «На повторе» — что слушаешь чаще всего последний месяц;
//   forgotten   «Давно не звучало» — любимое, что не играло два месяца;
//   tag-<жанр>  «<Жанр> микс» — жанр по тегам Last.fm (tags.go) среди тех,
//               кого слушаешь: твои треки этих артистов и их хиты на Deezer.
//
// Пересчитываются при каждом открытии (данных у человека немного), порядок
// внутри дня не меняется.

const (
	mixMinTracks    = 5  // меньше — микса нет
	mixMaxTracks    = 50 // как у Spotify
	mixTagMixes     = 4  // жанровых — не больше
	mixTagArtists   = 10 // артистов в жанровом миксе
	mixPerArtist    = 5  // треков одного артиста в жанровом миксе
	mixOwnPerArtist = 3  // из них — твоих (слушал, лайкнул)
	mixTasteDays    = 90 // вкус — по прослушиваниям за столько дней и всем лайкам
	mixRepeatDays   = 30
	mixForgetDays   = 60
	mixTopArtists   = 30 // жанры — по столько самым слушаемым артистам
	mixLikeWeight   = 3  // лайк весит как три прослушивания
)

// mix — карточка микса. Название клиент составляет сам по kind и tag.
type mix struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`          // on_repeat | forgotten | tag
	Tag     string   `json:"tag,omitempty"` // для tag — имя тега Last.fm
	Artists []string `json:"artists"`       // главные исполнители — подпись карточки
	Covers  []string `json:"covers"`        // обложки для картинки, первая — главная
}

// tasteTrack — трек из истории или лайков человека.
type tasteTrack struct {
	deezer.Track
	plays, plays30, plays90 int
	last, liked             time.Time // нули — не слушал / не лайкал
}

// mixPlan — микс и то, из чего строить его треки.
type mixPlan struct {
	mix
	tracks  []deezer.Track // готовые (on-repeat, forgotten)
	artists []string       // для tag: ID артистов по весу
	own     map[string][]deezer.Track
}

var mixIDRe = regexp.MustCompile(`^(on-repeat|forgotten|tag-[a-z0-9-]{1,60})$`)

// tagSlug — тег в адресе: «russian hip-hop» → russian-hip-hop, «r&b» → r-b.
func tagSlug(tag string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(tag) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// tasteOf — всё, что человек слушал или лайкал, с правками админа; скрытые
// релизы — только админу в режиме админа.
func (s *Server) tasteOf(r *http.Request, userID string) ([]tasteTrack, error) {
	rows, err := s.DB.Query(`
		WITH p AS (
			SELECT deezer_id, (array_agg(meta ORDER BY played_at DESC))[1] AS meta, count(*) AS n,
			       count(*) FILTER (WHERE played_at > now() - $2::int * interval '1 day') AS n30,
			       count(*) FILTER (WHERE played_at > now() - $3::int * interval '1 day') AS n90,
			       max(played_at) AS last
			  FROM plays WHERE user_id = $1 GROUP BY deezer_id)
		SELECT COALESCE(l.meta, p.meta), COALESCE(p.n, 0), COALESCE(p.n30, 0), COALESCE(p.n90, 0), p.last, l.added_at
		  FROM p FULL JOIN (SELECT deezer_id, meta, added_at FROM liked_tracks WHERE user_id = $1) l USING (deezer_id)`,
		userID, mixRepeatDays, mixTasteDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tasteTrack
	for rows.Next() {
		var t tasteTrack
		var meta []byte
		var last, liked *time.Time
		if err := rows.Scan(&meta, &t.plays, &t.plays30, &t.plays90, &last, &liked); err != nil {
			return nil, err
		}
		if json.Unmarshal(meta, &t.Track) != nil || t.ID == "" {
			continue
		}
		if last != nil {
			t.last = *last
		}
		if liked != nil {
			t.liked = *liked
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tracks := make([]deezer.Track, len(out))
	for i := range out {
		tracks[i] = out[i].Track
	}
	s.overlayFor(nil, nil, tracks)
	show := showHidden(r)
	kept := out[:0]
	for i := range out {
		out[i].Track = tracks[i]
		if !tracks[i].Hidden || show {
			kept = append(kept, out[i])
		}
	}
	// Порядок строк из базы не задан — для одинаковых миксов при равных весах.
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept, nil
}

// dailyRand — случайность, одна и та же для человека и микса весь день.
func dailyRand(userID, mixID string, now time.Time) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(userID + "|" + mixID + "|" + now.UTC().Format("2006-01-02")))
	return rand.New(rand.NewPCG(h.Sum64(), 0))
}

// План миксов живёт минуту: им же проверяется контекст каждого
// прослушивания из микса (mixCard), а пересчёт — вся история человека.
// Лайк сбрасывает его сразу (forgetMixes): лайки меняют миксы заметно.
const mixesTTL = time.Minute

func mixesKey(userID string) string { return "mixes:" + userID + ":" }

func (s *Server) forgetMixes(userID string) { s.cache.forget(mixesKey(userID)) }

// planMixes — какие миксы есть у человека сейчас. Результат общий (кеш) —
// только читать.
func (s *Server) planMixes(r *http.Request, userID string) ([]mixPlan, error) {
	return cached(&s.cache, fmt.Sprintf("%s%t", mixesKey(userID), showHidden(r)), mixesTTL, func() ([]mixPlan, error) {
		return s.computeMixes(r, userID)
	})
}

func (s *Server) computeMixes(r *http.Request, userID string) ([]mixPlan, error) {
	taste, err := s.tasteOf(r, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var plans []mixPlan
	if p, ok := onRepeatMix(taste, now); ok {
		plans = append(plans, p)
	}
	if p, ok := forgottenMix(taste, now, dailyRand(userID, "forgotten", now)); ok {
		plans = append(plans, p)
	}
	weights, names := artistWeights(taste)
	top := topArtists(weights, mixTopArtists)
	tags, err := s.tagsOf(top)
	if err != nil {
		return nil, err
	}
	plans = append(plans, tagMixes(taste, weights, names, top, tags)...)
	distinctCovers(plans)
	return plans, nil
}

// distinctCovers: главная обложка у каждого микса своя, если есть из чего
// выбрать, — иначе на полке два одинаковых квадрата.
func distinctCovers(plans []mixPlan) {
	used := map[string]bool{}
	for i := range plans {
		c := plans[i].Covers
		if j := slices.IndexFunc(c, func(u string) bool { return !used[u] }); j > 0 {
			c[0], c[j] = c[j], c[0]
		}
		if len(c) > 0 {
			used[c[0]] = true
		}
	}
}

func onRepeatMix(taste []tasteTrack, now time.Time) (mixPlan, bool) {
	var picked []tasteTrack
	for _, t := range taste {
		if t.plays30 >= 2 {
			picked = append(picked, t)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].plays30 != picked[j].plays30 {
			return picked[i].plays30 > picked[j].plays30
		}
		return picked[i].last.After(picked[j].last)
	})
	return finishPlan(mix{ID: "on-repeat", Kind: "on_repeat"}, picked)
}

// forgottenMix — лайкнутое или слушанное хотя бы трижды, но не игравшее
// mixForgetDays дней (и лайкнутое не вчера). Порядок — на день вперемешку.
func forgottenMix(taste []tasteTrack, now time.Time, rnd *rand.Rand) (mixPlan, bool) {
	cut := now.AddDate(0, 0, -mixForgetDays)
	var picked []tasteTrack
	for _, t := range taste {
		loved := !t.liked.IsZero() && t.liked.Before(cut) || t.plays >= 3
		if loved && t.last.Before(cut) {
			picked = append(picked, t)
		}
	}
	rnd.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
	return finishPlan(mix{ID: "forgotten", Kind: "forgotten"}, picked)
}

// finishPlan: не больше mixMaxTracks треков, подпись — частые исполнители.
func finishPlan(m mix, picked []tasteTrack) (mixPlan, bool) {
	if len(picked) < mixMinTracks {
		return mixPlan{}, false
	}
	picked = picked[:min(len(picked), mixMaxTracks)]
	tracks := make([]deezer.Track, len(picked))
	count := map[string]int{}
	names := map[string]string{}
	var order []string
	for i, t := range picked {
		tracks[i] = t.Track
		if t.ArtistID == "" {
			continue
		}
		if count[t.ArtistID] == 0 {
			order = append(order, t.ArtistID)
			names[t.ArtistID] = t.Artist
		}
		count[t.ArtistID]++
	}
	sort.SliceStable(order, func(i, j int) bool { return count[order[i]] > count[order[j]] })
	m.Artists = []string{}
	for _, id := range order[:min(len(order), 3)] {
		m.Artists = append(m.Artists, names[id])
	}
	m.Covers = coversOf(tracks)
	return mixPlan{mix: m, tracks: tracks}, true
}

// coversOf — до четырёх разных обложек.
func coversOf(tracks []deezer.Track) []string {
	covers := []string{}
	for _, t := range tracks {
		if t.CoverURL != "" && !slices.Contains(covers, t.CoverURL) {
			covers = append(covers, t.CoverURL)
			if len(covers) == 4 {
				break
			}
		}
	}
	return covers
}

// artistWeights — насколько человек слушает каждого основного исполнителя.
func artistWeights(taste []tasteTrack) (map[string]int, map[string]string) {
	w := map[string]int{}
	names := map[string]string{}
	for _, t := range taste {
		if t.ArtistID == "" || t.ArtistID == "0" {
			continue
		}
		n := t.plays90
		if !t.liked.IsZero() {
			n += mixLikeWeight
		}
		if n == 0 {
			continue
		}
		w[t.ArtistID] += n
		names[t.ArtistID] = t.Artist
	}
	return w, names
}

func topArtists(w map[string]int, n int) []string {
	ids := make([]string, 0, len(w))
	for id := range w {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if w[ids[i]] != w[ids[j]] {
			return w[ids[i]] > w[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids[:min(len(ids), n)]
}

// tagMixes — жанры, в которых у человека хотя бы два артиста; вес жанра —
// сумма весов артистов, умноженных на вес тега у артиста. Жанр, чьи
// артисты почти те же, что у уже выбранного («rap» после «hip-hop»), —
// повтор, его пропускаем.
func tagMixes(taste []tasteTrack, weights map[string]int, names map[string]string, top []string, tags map[string][]artistTag) []mixPlan {
	score := map[string]float64{}
	members := map[string][]string{}
	for _, id := range top { // top — по убыванию веса, так и члены жанра
		for _, t := range tags[id] {
			score[t.Name] += float64(weights[id]) * float64(t.Count) / 100
			members[t.Name] = append(members[t.Name], id)
		}
	}
	var order []string
	for name, ids := range members {
		if len(ids) >= 2 {
			order = append(order, name)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if score[order[i]] != score[order[j]] {
			return score[order[i]] > score[order[j]]
		}
		return order[i] < order[j]
	})

	own := map[string][]deezer.Track{} // артист → его треки у человека, частые сначала
	byArtist := map[string][]tasteTrack{}
	for _, t := range taste {
		byArtist[t.ArtistID] = append(byArtist[t.ArtistID], t)
	}
	for id, ts := range byArtist {
		sort.SliceStable(ts, func(i, j int) bool {
			if ts[i].plays != ts[j].plays {
				return ts[i].plays > ts[j].plays
			}
			return ts[i].liked.After(ts[j].liked)
		})
		for _, t := range ts[:min(len(ts), mixOwnPerArtist)] {
			own[id] = append(own[id], t.Track)
		}
	}

	var plans []mixPlan
	var chosen [][]string
	for _, name := range order {
		if len(plans) == mixTagMixes {
			break
		}
		ids := members[name]
		if slices.ContainsFunc(chosen, func(c []string) bool { return overlap(c, ids) >= 0.7 }) {
			continue
		}
		chosen = append(chosen, ids)
		ids = ids[:min(len(ids), mixTagArtists)]
		m := mix{ID: "tag-" + tagSlug(name), Kind: "tag", Tag: name, Artists: []string{}}
		var sample []deezer.Track
		for _, id := range ids {
			if len(m.Artists) < 3 {
				m.Artists = append(m.Artists, names[id])
			}
			sample = append(sample, own[id]...)
		}
		m.Covers = coversOf(sample)
		plans = append(plans, mixPlan{mix: m, artists: ids, own: own})
	}
	return plans
}

// overlap — доля общих артистов (от меньшего из двух списков).
func overlap(a, b []string) float64 {
	n := 0
	for _, x := range a {
		if slices.Contains(b, x) {
			n++
		}
	}
	return float64(n) / float64(min(len(a), len(b)))
}

// tagMixTracks — треки жанрового микса: по кругу от каждого артиста (самые
// слушаемые первыми) — твои его треки и хиты Deezer, на день вперемешку.
func (s *Server) tagMixTracks(p mixPlan, rnd *rand.Rand) []deezer.Track {
	tops := make([][]deezer.Track, len(p.artists))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, id := range p.artists {
		if isLocalID(id) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			guard("mix artist top", func() error {
				if top, err := s.artistTop(id); err == nil {
					tops[i] = slices.Clone(top) // кеш общий — не портить
				}
				return nil
			})
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	per := make([][]deezer.Track, len(p.artists))
	for i, id := range p.artists {
		for _, t := range p.own[id] {
			if !seen[t.ID] {
				seen[t.ID] = true
				per[i] = append(per[i], t)
			}
		}
		top := tops[i]
		rnd.Shuffle(len(top), func(a, b int) { top[a], top[b] = top[b], top[a] })
		for _, t := range top {
			if len(per[i]) >= mixPerArtist {
				break
			}
			if !seen[t.ID] {
				seen[t.ID] = true
				per[i] = append(per[i], t)
			}
		}
	}
	return interleave(per, mixMaxTracks)
}

// interleave — по кругу: первый трек каждого артиста, потом второй…, не
// больше max: артисты чередуются, а не идут блоками. Трек, который уже
// есть (совместный — в топе у обоих), второй раз не берётся: повтор в
// списке ронял страницу жанра (ключи строк на фронте — ID трека).
func interleave(per [][]deezer.Track, max int) []deezer.Track {
	var out []deezer.Track
	seen := map[string]bool{}
	for round := 0; len(out) < max; round++ {
		more := false
		for i := range per {
			if round < len(per[i]) && len(out) < max {
				more = true
				if t := per[i][round]; !seen[t.ID] {
					seen[t.ID] = true
					out = append(out, t)
				}
			}
		}
		if !more {
			break
		}
	}
	return out
}

// GET /api/me/mixes — миксы человека для главной.
func (s *Server) handleMixes(w http.ResponseWriter, r *http.Request) {
	plans, err := s.planMixes(r, currentUser(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	mixes := []mix{}
	for _, p := range plans {
		mixes = append(mixes, p.mix)
	}
	writeJSON(w, http.StatusOK, map[string]any{"mixes": mixes})
}

var errNoMix = errors.New("no such mix")

// mixTracks — микс id человека и его треки; errNoMix — такого сейчас нет.
func (s *Server) mixTracks(r *http.Request, userID, id string) (mix, []deezer.Track, error) {
	if !mixIDRe.MatchString(id) {
		return mix{}, nil, errNoMix
	}
	plans, err := s.planMixes(r, userID)
	if err != nil {
		return mix{}, nil, err
	}
	i := slices.IndexFunc(plans, func(p mixPlan) bool { return p.ID == id })
	if i < 0 {
		return mix{}, nil, errNoMix
	}
	p := plans[i]
	if p.Kind != "tag" {
		return p.mix, p.tracks, nil
	}
	// Копия: треки человека в плане общие с кешем, а overlay правит на месте.
	tracks := cloneTracks(s.tagMixTracks(p, dailyRand(userID, id, time.Now())))
	s.overlayFor(nil, nil, tracks)
	return p.mix, visibleTracks(r, tracks), nil
}

// mixCard — карточка микса id, если он сейчас есть у человека.
func (s *Server) mixCard(r *http.Request, userID, id string) (mix, bool) {
	if !mixIDRe.MatchString(id) {
		return mix{}, false
	}
	plans, err := s.planMixes(r, userID)
	if err != nil {
		return mix{}, false
	}
	i := slices.IndexFunc(plans, func(p mixPlan) bool { return p.ID == id })
	if i < 0 {
		return mix{}, false
	}
	return plans[i].mix, true
}

// GET /api/me/mixes/{id} — микс с треками.
func (s *Server) handleMix(w http.ResponseWriter, r *http.Request) {
	m, tracks, err := s.mixTracks(r, currentUser(r), r.PathValue("id"))
	if errors.Is(err, errNoMix) {
		writeError(w, http.StatusNotFound, "mix not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if tracks == nil {
		tracks = []deezer.Track{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mix": m, "tracks": tracks})
}
