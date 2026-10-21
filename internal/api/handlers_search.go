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
		return res, nil
	})
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
