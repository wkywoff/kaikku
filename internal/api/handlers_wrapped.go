package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// Итоги года («Wrapped»): сколько человек слушал, его топ треков,
// исполнителей и альбомов, по месяцам и часам, самый длинный ряд дней,
// первый трек года и место среди слушателей главного исполнителя.
//
// Как считается, чтобы не тормозило и через годы истории:
//   - прослушивания года (plays, 0011) читаются одним запросом по индексу
//     (user_id, played_at) — только нужные поля, без снимков целиком, — и
//     считаются в Go за один проход (20 тысяч записей — десятки мс);
//   - полные снимки треков — только для топ-50, вторым запросом;
//   - место среди слушателей — по индексу (artist_id, played_at) из 0014;
//   - готовые итоги в кеше: прошлые годы — на сутки (они уже не меняются),
//     текущий — на 10 минут.
//
// Минуты — длина трека из снимка (прослушивание пишется после 30 с), то
// есть оценка: перемотанное и брошенное на середине считается целиком.
// Часы, дни и границы года — в часовом поясе человека (?tz= из браузера).

const (
	wrappedTopTracks  = 50 // столько уходит в «Играть» и «Сохранить плейлистом»
	wrappedTopArtists = 5
	wrappedTopAlbums  = 5
	wrappedPastTTL    = 24 * time.Hour
	wrappedCurrentTTL = 10 * time.Minute
)

type wrappedTrack struct {
	deezer.Track
	Plays int `json:"plays"`
}

type wrappedArtist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url"`
	Plays      int    `json:"plays"`
	Minutes    int    `json:"minutes"`
}

type wrappedAlbum struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
	Plays    int    `json:"plays"`
}

type wrappedDay struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Minutes int    `json:"minutes"`
}

type wrappedListeners struct {
	Rank      int `json:"rank"`      // место по прослушиваниям главного исполнителя (1 — больше всех)
	Listeners int `json:"listeners"` // сколько человек на сервере его слушали за год
}

type wrappedStats struct {
	Year       int               `json:"year"`
	Years      []int             `json:"years"`           // годы, за которые есть история
	Since      string            `json:"since,omitempty"` // первое прослушивание года
	Plays      int               `json:"plays"`
	Minutes    int               `json:"minutes"`
	Tracks     int               `json:"tracks"`  // разных треков
	Artists    int               `json:"artists"` // разных исполнителей
	TopTracks  []wrappedTrack    `json:"top_tracks"`
	TopArtists []wrappedArtist   `json:"top_artists"`
	TopAlbums  []wrappedAlbum    `json:"top_albums"`
	Months     [12]int           `json:"months"` // минуты
	Hours      [24]int           `json:"hours"`  // прослушивания
	TopDay     *wrappedDay       `json:"top_day,omitempty"`
	Streak     int               `json:"streak"` // самый длинный ряд дней подряд
	First      *deezer.Track     `json:"first,omitempty"`
	Listeners  *wrappedListeners `json:"top_artist_listeners,omitempty"`
}

func (s *Server) handleWrapped(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	loc, err := time.LoadLocation(r.URL.Query().Get("tz"))
	if err != nil || r.URL.Query().Get("tz") == "" {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	year := now.Year()
	if y := r.URL.Query().Get("year"); y != "" {
		if year, err = strconv.Atoi(y); err != nil || year < 2000 || year > now.Year() {
			writeError(w, http.StatusBadRequest, "invalid year")
			return
		}
	}
	ttl := wrappedPastTTL
	if year == now.Year() {
		ttl = wrappedCurrentTTL
	}
	stats, err := cached(&s.cache, "wrapped:"+userID+":"+strconv.Itoa(year)+":"+loc.String(), ttl, func() (*wrappedStats, error) {
		return s.computeWrapped(userID, year, loc)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type wrappedPlay struct {
	trackID, title, artist, artistID, album, albumID, cover, isrc string
	durationMs                                                    int
	at                                                            time.Time
}

// wrappedCounter — прослушивания трека, исполнителя или альбома.
type wrappedCounter struct {
	plays, ms int
	p         wrappedPlay // последнее прослушивание — имя, обложка
}

// Одна запись на разных релизах (сингл, альбом, делюкс) у Deezer — разные
// треки; в итогах это одна песня (mergeRecordings).
const sameRecordingDurationMs = 3000

// mergeRecordings joins the counters of one recording released on several
// Deezer tracks: the same ISRC (in play snapshots since migration 0015) or —
// for older history without it — the same main artist, title and length
// (±3 s). The song is represented by its most played track. Returns the
// merged counters by representative track ID.
func mergeRecordings(byTrack map[string]*wrappedCounter) map[string]*wrappedCounter {
	ids := make([]string, 0, len(byTrack))
	for id := range byTrack {
		ids = append(ids, id)
	}
	sort.Strings(ids) // детерминированно, независимо от порядка обхода map

	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if p, ok := parent[x]; ok && p != x {
			r := find(p)
			parent[x] = r
			return r
		}
		return x
	}
	union := func(a, b string) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[rb] = ra
		}
	}
	type signature struct{ artist, title string }
	byISRC := map[string]string{}
	bySig := map[signature][]string{}
	for _, id := range ids {
		c := byTrack[id]
		if c.p.isrc != "" {
			if other, ok := byISRC[c.p.isrc]; ok {
				union(other, id)
			} else {
				byISRC[c.p.isrc] = id
			}
		}
		sig := signature{c.p.artistID, strings.ToLower(strings.TrimSpace(c.p.title))}
		for _, other := range bySig[sig] {
			if d := byTrack[other].p.durationMs - c.p.durationMs; d <= sameRecordingDurationMs && d >= -sameRecordingDurationMs {
				union(other, id)
				break
			}
		}
		bySig[sig] = append(bySig[sig], id)
	}

	groups := map[string][]string{}
	for _, id := range ids {
		root := find(id)
		groups[root] = append(groups[root], id)
	}
	merged := make(map[string]*wrappedCounter, len(groups))
	for _, members := range groups {
		rep := members[0]
		sum := &wrappedCounter{}
		for _, id := range members {
			c := byTrack[id]
			if c.plays > byTrack[rep].plays {
				rep = id
			}
			sum.plays += c.plays
			sum.ms += c.ms
		}
		sum.p = byTrack[rep].p
		merged[rep] = sum
	}
	return merged
}

func (s *Server) computeWrapped(userID string, year int, loc *time.Location) (*wrappedStats, error) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(1, 0, 0)
	st := &wrappedStats{Year: year, Years: []int{}, TopTracks: []wrappedTrack{}, TopArtists: []wrappedArtist{}, TopAlbums: []wrappedAlbum{}}

	var first, last sql.NullTime
	if err := s.DB.QueryRow(`SELECT min(played_at), max(played_at) FROM plays WHERE user_id = $1`, userID).Scan(&first, &last); err != nil {
		return nil, err
	}
	if first.Valid {
		for y := first.Time.In(loc).Year(); y <= last.Time.In(loc).Year(); y++ {
			st.Years = append(st.Years, y)
		}
	}

	rows, err := s.DB.Query(
		`SELECT deezer_id, played_at, COALESCE(meta->>'title', ''), COALESCE(meta->>'artist', ''),
		        COALESCE(meta->>'artist_id', ''), COALESCE(meta->>'album', ''), COALESCE(meta->>'album_id', ''),
		        COALESCE(meta->>'cover_url', ''), COALESCE((meta->>'duration_ms')::int, 0), COALESCE(meta->>'isrc', '')
		   FROM plays
		  WHERE user_id = $1 AND played_at >= $2 AND played_at < $3
		  ORDER BY played_at`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type counter = wrappedCounter
	tracks := map[string]*counter{}
	artists := map[string]*counter{}
	albums := map[string]*counter{}
	days := map[string]int{} // дата → мс
	var firstPlay *wrappedPlay
	var totalMs int64
	for rows.Next() {
		var p wrappedPlay
		if err := rows.Scan(&p.trackID, &p.at, &p.title, &p.artist, &p.artistID, &p.album, &p.albumID, &p.cover, &p.durationMs, &p.isrc); err != nil {
			return nil, err
		}
		if firstPlay == nil {
			fp := p
			firstPlay = &fp
		}
		st.Plays++
		totalMs += int64(p.durationMs)
		local := p.at.In(loc)
		st.Months[local.Month()-1] += p.durationMs
		st.Hours[local.Hour()]++
		days[local.Format("2006-01-02")] += p.durationMs
		bump := func(m map[string]*counter, key string) {
			if key == "" {
				return
			}
			c := m[key]
			if c == nil {
				c = &counter{}
				m[key] = c
			}
			c.plays++
			c.ms += p.durationMs
			c.p = p
		}
		bump(tracks, p.trackID)
		bump(artists, p.artistID)
		bump(albums, p.albumID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if st.Plays == 0 {
		return st, nil
	}
	for i := range st.Months {
		st.Months[i] /= 60000
	}
	st.Minutes = int(totalMs / 60000)
	tracks = mergeRecordings(tracks)
	st.Tracks, st.Artists = len(tracks), len(artists)
	st.Since = firstPlay.at.In(loc).Format("2006-01-02")
	st.TopDay, st.Streak = topDayAndStreak(days)

	// Топы: больше прослушиваний — выше; при равенстве — больше минут, потом по ключу (стабильно).
	top := func(m map[string]*counter, n int) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := m[keys[i]], m[keys[j]]
			if a.plays != b.plays {
				return a.plays > b.plays
			}
			if a.ms != b.ms {
				return a.ms > b.ms
			}
			return keys[i] < keys[j]
		})
		if len(keys) > n {
			keys = keys[:n]
		}
		return keys
	}

	topTrackIDs := top(tracks, wrappedTopTracks)
	metas, err := s.latestPlayMetas(userID, append(topTrackIDs, firstPlay.trackID))
	if err != nil {
		return nil, err
	}
	trackList := make([]deezer.Track, 0, len(topTrackIDs))
	for _, id := range topTrackIDs {
		if t, ok := metas[id]; ok {
			trackList = append(trackList, t)
		}
	}
	s.overlayFor(nil, nil, trackList) // правки админа, как в остальных списках
	for _, t := range trackList {
		st.TopTracks = append(st.TopTracks, wrappedTrack{Track: t, Plays: tracks[t.ID].plays})
	}
	if t, ok := metas[firstPlay.trackID]; ok {
		one := []deezer.Track{t}
		s.overlayFor(nil, nil, one)
		st.First = &one[0]
	}

	// Альбомы и исполнители — с правками админа (названия, обложки, фото),
	// как в остальных списках, а не как в снимках прослушиваний.
	topAlbumIDs := top(albums, wrappedTopAlbums)
	albumList := make([]deezer.Album, len(topAlbumIDs))
	for i, id := range topAlbumIDs {
		c := albums[id]
		albumList[i] = deezer.Album{ID: id, Title: c.p.album, ArtistID: c.p.artistID, ArtistName: c.p.artist, CoverURL: c.p.cover}
	}
	s.overlayFor(nil, albumPtrs(albumList), nil)
	for _, a := range albumList {
		st.TopAlbums = append(st.TopAlbums, wrappedAlbum{ID: a.ID, Title: a.Title, Artist: a.ArtistName, CoverURL: a.CoverURL, Plays: albums[a.ID].plays})
	}

	// Исполнители: имя и фото — текущие с Deezer (страница артиста в кеше),
	// поверх — правки админа; не вышло — имя из снимка прослушивания.
	topArtistIDs := top(artists, wrappedTopArtists)
	artistList := make([]deezer.Artist, len(topArtistIDs))
	var wg sync.WaitGroup
	for i, id := range topArtistIDs {
		artistList[i] = deezer.Artist{ID: id, Name: artists[id].p.artist}
		wg.Add(1)
		go func() {
			defer wg.Done()
			guard("wrapped artist", func() error {
				if a, err := s.catalogArtist(id); err == nil { // свой артист — из базы
					artistList[i].Name, artistList[i].PictureURL = a.Name, a.PictureURL
				}
				return nil
			})
		}()
	}
	wg.Wait()
	s.overlayFor(artistPtrs(artistList), nil, nil)
	for _, a := range artistList {
		c := artists[a.ID]
		st.TopArtists = append(st.TopArtists, wrappedArtist{ID: a.ID, Name: a.Name, PictureURL: a.PictureURL, Plays: c.plays, Minutes: c.ms / 60000})
	}
	if len(topArtistIDs) > 0 {
		st.Listeners, err = s.artistListenerRank(topArtistIDs[0], artists[topArtistIDs[0]].plays, from, to)
		if err != nil {
			return nil, err
		}
	}
	return st, nil
}

// latestPlayMetas returns the newest snapshot of each listed song.
func (s *Server) latestPlayMetas(userID string, ids []string) (map[string]deezer.Track, error) {
	rows, err := s.DB.Query(
		`SELECT DISTINCT ON (deezer_id) deezer_id, meta FROM plays
		  WHERE user_id = $1 AND deezer_id = ANY($2)
		  ORDER BY deezer_id, played_at DESC`, userID, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]deezer.Track{}
	for rows.Next() {
		var id string
		var meta []byte
		if err := rows.Scan(&id, &meta); err != nil {
			return nil, err
		}
		var t deezer.Track
		if json.Unmarshal(meta, &t) == nil {
			out[id] = t
		}
	}
	return out, rows.Err()
}

// artistListenerRank: on which place the user is among the server's
// listeners of the artist this year (by plays).
func (s *Server) artistListenerRank(artistID string, myPlays int, from, to time.Time) (*wrappedListeners, error) {
	var l wrappedListeners
	err := s.DB.QueryRow(
		`SELECT count(*) FILTER (WHERE n > $4) + 1, count(*)
		   FROM (SELECT user_id, count(*) AS n FROM plays
		          WHERE meta->>'artist_id' = $1 AND played_at >= $2 AND played_at < $3
		          GROUP BY user_id) x`, artistID, from, to, myPlays,
	).Scan(&l.Rank, &l.Listeners)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// topDayAndStreak: the day with the most music and the longest run of days in a row.
func topDayAndStreak(days map[string]int) (*wrappedDay, int) {
	dates := make([]string, 0, len(days))
	for d := range days {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	var best *wrappedDay
	bestMs, streak, run := 0, 0, 0
	var prev time.Time
	for i, d := range dates {
		if best == nil || days[d] > bestMs {
			best, bestMs = &wrappedDay{Date: d, Minutes: days[d] / 60000}, days[d]
		}
		t, _ := time.Parse("2006-01-02", d)
		if i > 0 && t.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		prev = t
		if run > streak {
			streak = run
		}
	}
	return best, streak
}
