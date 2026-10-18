package api

import (
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// Жанры — по тегам Last.fm (tags.go): плитки на пустой странице поиска и
// страница жанра /genre/<slug>. Знаем только тех артистов, которых на
// сервисе кто-то слушал, лайкал или на кого подписан, — зато страница
// жанра сразу показывает, что тут слушают другие.

const (
	genreTiles       = 20 // плиток на странице поиска
	genreOwnTracks   = 20
	genreArtists     = 12
	genreTopArtists  = 8 // чьи хиты Deezer — в «Ещё в жанре»
	genrePerArtist   = 5
	genreMaxTracks   = 40
	genresTTL        = 10 * time.Minute
	genrePageTTL     = 5 * time.Minute
	genreMinArtists  = 1
	genreServerLimit = 60
)

type genre struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// GET /api/genres — жанры для плиток: сначала твои (по весу, как миксы),
// потом самые частые на сервисе.
func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	list, err := cached(&s.cache, "genres:"+userID, genresTTL, func() ([]genre, error) {
		taste, err := s.tasteOf(r, userID)
		if err != nil {
			return nil, err
		}
		weights, _ := artistWeights(taste)
		top := topArtists(weights, mixTopArtists)
		tags, err := s.tagsOf(top)
		if err != nil {
			return nil, err
		}
		score := map[string]float64{}
		for _, id := range top {
			for _, t := range tags[id] {
				score[t.Name] += float64(weights[id]) * float64(t.Count) / 100
			}
		}
		mine := make([]string, 0, len(score))
		for name := range score {
			mine = append(mine, name)
		}
		sort.Slice(mine, func(i, j int) bool {
			if score[mine[i]] != score[mine[j]] {
				return score[mine[i]] > score[mine[j]]
			}
			return mine[i] < mine[j]
		})
		popular, err := s.idList(`
			SELECT t->>'name' FROM artist_tags, jsonb_array_elements(tags) t
			 GROUP BY 1 HAVING count(*) >= 2 ORDER BY count(*) DESC, 1 LIMIT $1`, genreTiles)
		if err != nil {
			return nil, err
		}
		out := []genre{}
		seen := map[string]bool{}
		for _, name := range append(mine, popular...) {
			slug := tagSlug(name)
			if slug == "" || seen[slug] || len(out) == genreTiles {
				continue
			}
			seen[slug] = true
			out = append(out, genre{Slug: slug, Name: name})
		}
		return out, nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"genres": list})
}

type genrePage struct {
	Genre   genre           `json:"genre"`
	Own     []deezer.Track  `json:"own"`     // твои треки жанра — что слушал и лайкал
	Artists []deezer.Artist `json:"artists"` // другие артисты жанра на сервисе
	Tracks  []deezer.Track  `json:"tracks"`  // их хиты на Deezer, вперемешку
	MixID   string          `json:"mix_id,omitempty"`
}

// GET /api/genres/{slug}
func (s *Server) handleGenre(w http.ResponseWriter, r *http.Request) {
	userID, slug := currentUser(r), r.PathValue("slug")
	page, err := cached(&s.cache, "genre:"+userID+":"+slug, genrePageTTL, func() (*genrePage, error) { return s.genrePage(r, userID, slug) })
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if page == nil {
		writeError(w, http.StatusNotFound, "genre not found")
		return
	}
	// Копии: правки админа накладываются на месте, а страница — в кеше.
	out := *page
	out.Own, out.Tracks, out.Artists = cloneTracks(page.Own), cloneTracks(page.Tracks), slices.Clone(page.Artists)
	s.overlayFor(artistPtrs(out.Artists), nil, append(out.Own, out.Tracks...))
	out.Own, out.Tracks = visibleTracks(r, out.Own), visibleTracks(r, out.Tracks)
	writeJSON(w, http.StatusOK, out)
}

// genrePage — nil, если такого жанра нет ни у одного артиста.
func (s *Server) genrePage(r *http.Request, userID, slug string) (*genrePage, error) {
	names, err := s.idList(`SELECT DISTINCT t->>'name' FROM artist_tags, jsonb_array_elements(tags) t`)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(names, func(n string) bool { return tagSlug(n) == slug })
	if i < 0 {
		return nil, nil
	}
	name := names[i]

	// Артисты жанра: самые «жанровые» (вес тега) первыми.
	rows, err := s.DB.Query(`
		SELECT a.deezer_id, COALESCE(a.name, '') FROM artist_tags a, jsonb_array_elements(a.tags) t
		 WHERE a.tags @> jsonb_build_array(jsonb_build_object('name', $1::text))
		   AND t->>'name' = $1
		 ORDER BY (t->>'count')::int DESC, a.deezer_id
		 LIMIT $2`, name, genreServerLimit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id, n string
		if rows.Scan(&id, &n) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) < genreMinArtists {
		return nil, nil
	}
	inGenre := map[string]bool{}
	for _, id := range ids {
		inGenre[id] = true
	}

	page := &genrePage{Genre: genre{Slug: slug, Name: name}, Own: []deezer.Track{}, Artists: []deezer.Artist{}, Tracks: []deezer.Track{}}

	// Своё: треки артистов жанра из истории и лайков, частые первыми.
	taste, err := s.tasteOf(r, userID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(taste, func(i, j int) bool {
		if taste[i].plays != taste[j].plays {
			return taste[i].plays > taste[j].plays
		}
		return taste[i].liked.After(taste[j].liked)
	})
	mine := map[string]bool{}
	for _, t := range taste {
		if inGenre[t.ArtistID] {
			mine[t.ArtistID] = true
			if len(page.Own) < genreOwnTracks {
				page.Own = append(page.Own, t.Track)
			}
		}
	}

	// Другие артисты жанра — с фото (Deezer, в кеше страниц артистов).
	var others []string
	for _, id := range ids {
		if !mine[id] && !isLocalID(id) && len(others) < genreArtists {
			others = append(others, id)
		}
	}
	artists := make([]*deezer.Artist, len(others))
	tops := make([][]deezer.Track, len(others))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, id := range others {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			guard("genre artist", func() error {
				if a, err := s.artistInfo(id); err == nil {
					artists[i] = &a
				}
				if i < genreTopArtists {
					if top, err := s.artistTop(id); err == nil {
						tops[i] = slices.Clone(top[:min(len(top), genrePerArtist)])
					}
				}
				return nil
			})
		}()
	}
	wg.Wait()
	for _, a := range artists {
		if a != nil {
			page.Artists = append(page.Artists, *a)
		}
	}
	if t := interleave(tops, genreMaxTracks); t != nil {
		page.Tracks = t
	}

	if plans, err := s.planMixes(r, userID); err == nil {
		if slices.ContainsFunc(plans, func(p mixPlan) bool { return p.ID == "tag-"+slug }) {
			page.MixID = "tag-" + slug
		}
	}
	return page, nil
}
