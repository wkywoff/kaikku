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
		res.tracks = s.tracksFromFreshReleases(q, res.tracks, res.artists)
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
	found := s.albumTracksMatching(q, picked, func(t deezer.Track) bool { return hasAllWords(words, t.Title, t.Artist) })
	return prependNew(found, tracks)
}

// Свежий релиз Deezer добавляет в поиск через несколько дней, а в список
// релизов артиста — сразу.
const (
	freshReleaseAge   = 60 * 24 * time.Hour
	freshReleasesOpen = 2   // сколько свежих релизов открывать
	freshArtists      = 2   // у скольких найденных артистов смотреть
	freshReleasesPage = 100 // одна страница списка релизов — один запрос
)

// tracksFromFreshReleases ищет трек среди свежих релизов артиста, если ни
// поиск, ни найденные альбомы подходящего не дали: «5opka русский канье» в
// день выхода сингла — Deezer уже знает артиста и релиз, но поиск его ещё не
// находит. Срабатывает, только когда в запросе есть имя артиста и кроме
// имени что-то ещё; смотрит релизы за freshReleaseAge, в названии которых
// есть остальные слова. Список релизов берётся у страницы артиста (её кеш)
// или одной страницей и кешируется на час, треклисты — кеш Deezer: пока
// человек печатает запрос по буквам, Deezer спрашивают один-два раза.
func (s *Server) tracksFromFreshReleases(q string, tracks []deezer.Track, artists []deezer.Artist) []deezer.Track {
	lq := strings.ToLower(q)
	words := strings.Fields(lq)
	if len(words) < 2 || slices.ContainsFunc(tracks, func(t deezer.Track) bool { return hasAllWords(words, t.Title, t.Artist) }) {
		return tracks
	}
	for _, a := range s.artistsNamedIn(q, lq, words, artists) {
		rest := strings.Fields(strings.Replace(lq, strings.ToLower(strings.TrimSpace(a.Name)), " ", 1))
		if len(rest) == 0 {
			continue // запрос — одно имя: его треки и так в выдаче
		}
		releases, err := s.freshReleases(a.ID)
		if err != nil {
			log.Printf("search %q: releases of artist %s: %v", q, a.ID, err)
			continue
		}
		var picked []deezer.Album
		for _, r := range releases {
			if len(picked) < freshReleasesOpen && slices.ContainsFunc(rest, func(w string) bool { return strings.Contains(strings.ToLower(r.Title), w) }) {
				picked = append(picked, r)
			}
		}
		found := s.albumTracksMatching(q, picked, func(t deezer.Track) bool { return hasAllWords(words, t.Title, t.Artist, a.Name) })
		if len(found) > 0 {
			return prependNew(found, tracks)
		}
	}
	return tracks
}

// artistsNamedIn — до freshArtists артистов, чьё имя целиком есть в запросе.
// Сначала из выдачи поиска (бесплатно), но на «артист название» Deezer её
// обычно не даёт: он ищет артиста по всем словам. Тогда — поиск артистов по
// первому слову, а если в запросе от трёх слов — и по двум первым (имя
// пишут первым). Эти поиски кешируются: пока дописывается название, первое
// слово то же, и Deezer его больше не спрашивают.
func (s *Server) artistsNamedIn(q, lq string, words []string, found []deezer.Artist) []deezer.Artist {
	named := func(artists []deezer.Artist) []deezer.Artist {
		var out []deezer.Artist
		for _, a := range artists {
			name := strings.ToLower(strings.TrimSpace(a.Name))
			if len(out) < freshArtists && len([]rune(name)) >= 2 && strings.Contains(lq, name) && validDeezerID(a.ID) {
				out = append(out, a)
			}
		}
		return out
	}
	if out := named(found); len(out) > 0 {
		return out
	}
	for n := 1; n <= 2 && n < len(words); n++ {
		if n == 2 && len(words) < 3 {
			break // второе слово ещё дописывается — по нему каждый раз новый запрос
		}
		prefix := strings.Join(words[:n], " ")
		artists, err := cached(&s.cache, "search-artists:"+prefix, searchTTL, func() ([]deezer.Artist, error) {
			// Человек печатал запрос по буквам — поиск по одному этому слову
			// уже был и лежит в кеше вместе с артистами.
			if res, ok := peekCached[searchResult](&s.cache, "search:"+prefix); ok {
				return res.artists, nil
			}
			return s.Deezer.SearchArtists(prefix, 10)
		})
		if err != nil {
			log.Printf("search %q: artists named %q: %v", q, prefix, err)
			return nil
		}
		if out := named(artists); len(out) > 0 {
			return out
		}
	}
	return nil
}

// freshReleases — релизы артиста за freshReleaseAge, новые первыми.
func (s *Server) freshReleases(artistID string) ([]deezer.Album, error) {
	all, ok := peekCached[[]deezer.Album](&s.cache, "discography:"+artistID) // открывали страницу артиста
	if !ok {
		var err error
		all, err = storedCached(s, "releases:"+artistID, discographyTTL, func() ([]deezer.Album, error) {
			return s.Deezer.GetArtistAlbums(artistID, freshReleasesPage)
		})
		if err != nil {
			return nil, err
		}
	}
	since := time.Now().Add(-freshReleaseAge).Format("2006-01-02")
	var fresh []deezer.Album
	for _, a := range all {
		if a.ReleaseDate >= since { // даты Deezer — YYYY-MM-DD, сравниваются строками
			fresh = append(fresh, a)
		}
	}
	slices.SortStableFunc(fresh, func(a, b deezer.Album) int { return strings.Compare(b.ReleaseDate, a.ReleaseDate) })
	return fresh, nil
}

// albumTracksMatching — треки альбомов (треклисты параллельно), подходящие
// под match, без повторов. Альбом, который не открылся, пропускается.
func (s *Server) albumTracksMatching(q string, albums []deezer.Album, match func(deezer.Track) bool) []deezer.Track {
	lists := make([][]deezer.Track, len(albums))
	var wg sync.WaitGroup
	for i := range albums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := guard("search album tracks", func() (err error) {
				lists[i], err = s.Deezer.GetAlbumTracks(&albums[i], 100)
				return
			}); err != nil {
				log.Printf("search %q: tracks of album %s: %v", q, albums[i].ID, err)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	var found []deezer.Track
	for _, list := range lists {
		for _, t := range list {
			if !seen[t.ID] && match(t) {
				seen[t.ID] = true
				found = append(found, t)
			}
		}
	}
	return found
}

// prependNew — found, а за ним треки из tracks, которых в found нет.
func prependNew(found, tracks []deezer.Track) []deezer.Track {
	if len(found) == 0 {
		return tracks
	}
	out := slices.Clone(found)
	for _, t := range tracks {
		if !slices.ContainsFunc(found, func(f deezer.Track) bool { return f.ID == t.ID }) {
			out = append(out, t)
		}
	}
	return out
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
