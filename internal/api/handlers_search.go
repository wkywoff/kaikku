package api

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// handleSearch is the one search box: tracks, artists and albums from
// Deezer, fetched concurrently, with own uploads (local.go) first. Own
// results are shown even when Deezer is down.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, `missing query param "q"`)
		return
	}

	var (
		found     searchResult
		deezerErr error
		done      = make(chan struct{})
	)
	go func() {
		defer close(done)
		found, deezerErr = s.searchDeezer(q)
	}()
	own, ownErr := s.searchLocal(viewerOf(r), q, 10)
	if ownErr != nil {
		log.Printf("search %q: own uploads: %v", q, ownErr)
	}
	<-done
	// Человек печатает дальше — фронт отменил этот запрос.
	if r.Context().Err() != nil {
		return
	}
	if deezerErr != nil {
		if own.empty() {
			writeDeezerError(w, "deezer search failed", deezerErr)
			return
		}
		log.Printf("search %q: deezer failed, own uploads only: %v", q, deezerErr)
	}
	// Копии: правки админа накладываются на месте, а ответ Deezer — в кеше.
	tracks, artists, albums := cloneTracks(found.tracks), slices.Clone(found.artists), cloneAlbums(found.albums)
	// Фиты — из кеша, остальные догрузятся фоном (enrich.go): запрос к Deezer
	// на каждый трек выдачи делал поиск в ~8 раз дороже, а печатают его на ходу.
	s.enrichLater(s.Deezer.EnrichContributorsCached(tracks))
	s.overlayFor(artistPtrs(artists), albumPtrs(albums), tracks)
	// Пустой список — [], не null (append к nil пустого даёт nil): фронт
	// читает artists.length.
	tracks = append(append([]deezer.Track{}, own.tracks...), visibleTracks(r, tracks)...)
	artists = append(append([]deezer.Artist{}, own.artists...), artists...)
	albums = append(append([]deezer.Album{}, visibleAlbums(r, own.albums)...), visibleAlbums(r, albums)...) // версии — на странице основного
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks, "artists": artists, "albums": albums,
		"users": s.searchPeople(currentUser(r), q)})
}

// searchResult — выдача Deezer на запрос, как он её отдал (без фитов).
type searchResult struct {
	tracks  []deezer.Track
	artists []deezer.Artist
	albums  []deezer.Album
}

// Одинаковые запросы подряд (человек стёр букву и вернул, открыл результат
// и вернулся назад) не тратят лимит Deezer: три запроса и ещё по одному на
// каждый трек за фитами.
const searchTTL = 5 * time.Minute

// searchDeezer — треки, артисты и альбомы параллельно. Не кешируется ни
// частичная выдача (что-то из трёх не ответило), ни отменённая.
func (s *Server) searchDeezer(q string) (searchResult, error) {
	key := "search:" + strings.ToLower(strings.TrimSpace(q))
	return cached(&s.cache, key, searchTTL, func() (searchResult, error) {
		var res searchResult
		var errs [3]error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			errs[0] = guard("search tracks", func() (err error) { res.tracks, err = s.Deezer.Search(q, 20); return })
		}()
		go func() {
			defer wg.Done()
			errs[1] = guard("search artists", func() (err error) { res.artists, err = s.Deezer.SearchArtists(q, 10); return })
		}()
		go func() {
			defer wg.Done()
			errs[2] = guard("search albums", func() (err error) { res.albums, err = s.Deezer.SearchAlbums(q, 10); return })
		}()
		wg.Wait()
		if err := errors.Join(errs[:]...); err != nil {
			return searchResult{}, err
		}
		res.tracks = s.tracksFromAlbums(q, res.tracks, res.albums)
		return res, nil
	})
}

// albumsForTracks — из скольких найденных альбомов добирать треки.
const albumsForTracks = 3

// tracksFromAlbums добирает треки из найденных альбомов, если поиск треков
// Deezer не нашёл ни одного подходящего. У небольших артистов Deezer ищет
// альбомы, а их треков в поиске треков нет вовсе: «mortrivial time
// distortion» находит альбом TIME DISTORTION, но не трек, а на «mortrivial»
// отдаёт треки Mortician. Тогда из первых альбомов, к которым относится
// хоть одно слово запроса, берутся треки, где каждое слово есть в названии
// или исполнителе, — они идут первыми. Есть подходящий трек — ничего не
// добираем: это лишние запросы к Deezer.
func (s *Server) tracksFromAlbums(q string, tracks []deezer.Track, albums []deezer.Album) []deezer.Track {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 || slices.ContainsFunc(tracks, func(t deezer.Track) bool { return hasAllWords(words, t.Title, t.Artist) }) {
		return tracks
	}
	var picked []deezer.Album
	for _, a := range albums {
		if len(picked) == albumsForTracks {
			break
		}
		if text := strings.ToLower(a.Title + " " + a.ArtistName); slices.ContainsFunc(words, func(w string) bool { return strings.Contains(text, w) }) {
			picked = append(picked, a)
		}
	}
	lists := make([][]deezer.Track, len(picked))
	var wg sync.WaitGroup
	for i := range picked {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Не вышло — остаётся то, что нашёл поиск треков.
			if err := guard("search album tracks", func() (err error) {
				lists[i], err = s.Deezer.GetAlbumTracks(&picked[i], 100)
				return
			}); err != nil {
				log.Printf("search %q: tracks of album %s: %v", q, picked[i].ID, err)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	var found []deezer.Track
	for _, list := range lists {
		for _, t := range list {
			if !seen[t.ID] && hasAllWords(words, t.Title, t.Artist) {
				seen[t.ID] = true
				found = append(found, t)
			}
		}
	}
	if len(found) == 0 {
		return tracks
	}
	for _, t := range tracks {
		if !seen[t.ID] {
			found = append(found, t)
		}
	}
	return found
}

// hasAllWords — каждое слово (уже в нижнем регистре) есть хотя бы в одном из полей.
func hasAllWords(words []string, fields ...string) bool {
	text := strings.ToLower(strings.Join(fields, " "))
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// handleSearchArtists — артисты для выбора исполнителей при загрузке
// (uploads.go): свои, потом Deezer. Один запрос к Deezer, а не вся выдача
// поиска: поле ищет на ходу, пока человек печатает.
func (s *Server) handleSearchArtists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, `missing query param "q"`)
		return
	}
	own, err := s.searchOwnArtists(viewerOf(r), q, 6, true)
	if err != nil {
		log.Printf("artist search %q: own: %v", q, err)
	}
	found, err := s.Deezer.SearchArtists(q, 8)
	if err != nil {
		if len(own) == 0 {
			writeDeezerError(w, "deezer search failed", err)
			return
		}
		log.Printf("artist search %q: deezer: %v", q, err)
	}
	s.overlayFor(artistPtrs(found), nil, nil)
	if own == nil {
		own = []deezer.Artist{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"artists": append(own, found...)})
}
