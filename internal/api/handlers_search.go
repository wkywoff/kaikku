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
	tracks, artists, albums := cloneTracks(found.tracks), slices.Clone(found.artists), cloneAlbums(found.albums)
	s.enrichLater(s.Deezer.EnrichContributorsCached(tracks))
	s.overlayFor(artistPtrs(artists), albumPtrs(albums), tracks)
	tracks = append(append([]deezer.Track{}, own.tracks...), visibleTracks(r, tracks)...)
	artists = append(append([]deezer.Artist{}, own.artists...), artists...)
	albums = append(append([]deezer.Album{}, visibleAlbums(r, own.albums)...), visibleAlbums(r, albums)...)
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks, "artists": artists, "albums": albums,
		"users": s.searchPeople(currentUser(r), q)})
}

type searchResult struct {
	tracks  []deezer.Track
	artists []deezer.Artist
	albums  []deezer.Album
}

const searchTTL = 5 * time.Minute

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

const albumsForTracks = 3

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

const (
	freshReleaseAge   = 60 * 24 * time.Hour
	freshReleasesOpen = 2
	freshArtists      = 2
	freshReleasesPage = 100
)

func (s *Server) tracksFromFreshReleases(q string, tracks []deezer.Track, artists []deezer.Artist) []deezer.Track {
	lq := strings.ToLower(q)
	words := strings.Fields(lq)
	if len(words) < 2 || slices.ContainsFunc(tracks, func(t deezer.Track) bool { return hasAllWords(words, t.Title, t.Artist) }) {
		return tracks
	}
	for _, a := range s.artistsNamedIn(q, lq, words, artists) {
		rest := strings.Fields(strings.Replace(lq, strings.ToLower(strings.TrimSpace(a.Name)), " ", 1))
		if len(rest) == 0 {
			continue
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
			break
		}
		prefix := strings.Join(words[:n], " ")
		artists, err := cached(&s.cache, "search-artists:"+prefix, searchTTL, func() ([]deezer.Artist, error) {
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

func (s *Server) freshReleases(artistID string) ([]deezer.Album, error) {
	all, ok := peekCached[[]deezer.Album](&s.cache, "discography:"+artistID)
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
		if a.ReleaseDate >= since {
			fresh = append(fresh, a)
		}
	}
	slices.SortStableFunc(fresh, func(a, b deezer.Album) int { return strings.Compare(b.ReleaseDate, a.ReleaseDate) })
	return fresh, nil
}

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

func hasAllWords(words []string, fields ...string) bool {
	text := strings.ToLower(strings.Join(fields, " "))
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

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
