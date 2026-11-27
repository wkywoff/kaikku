package api

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

type playContext struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s *Server) handleRecordPlay(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	var req struct {
		DeezerID string      `json:"deezer_id"`
		Context  playContext `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validCatalogID(req.DeezerID) {
		writeError(w, http.StatusBadRequest, "deezer_id must be a song ID")
		return
	}
	track, err := s.catalogTrack(viewerOf(r), req.DeezerID)
	if err != nil {
		writeCatalogError(w, "track lookup failed", err)
		return
	}
	ctx, ctxMeta := s.resolvePlayContext(r, userID, req.Context, track)
	if ctx.Kind == "album" && ctx.ID != track.AlbumID {
		s.asTracklistOf(ctx.ID, track)
	}
	meta, _ := json.Marshal(track)
	var id int64
	if err := s.DB.QueryRow(
		`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, context_meta) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		userID, req.DeezerID, meta, ctx.Kind, ctx.ID, ctxMeta,
	).Scan(&id); err != nil {
		serverError(w, r, "could not record the play", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handlePlayListened(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Ms int `json:"ms"`
	}
	if err != nil || json.NewDecoder(r.Body).Decode(&req) != nil || req.Ms < 0 {
		writeError(w, http.StatusBadRequest, "ms must be a number of milliseconds")
		return
	}
	res, err := s.DB.Exec(
		`UPDATE plays SET listened_ms = GREATEST(COALESCE(listened_ms, 0),
		        CASE WHEN COALESCE((meta->>'duration_ms')::int, 0) > 0 THEN LEAST($1, (meta->>'duration_ms')::int) ELSE $1 END)
		  WHERE id = $2 AND user_id = $3`, req.Ms, id, currentUser(r))
	if err != nil {
		serverError(w, r, "could not save the play", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "no such play")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) asTracklistOf(albumID string, track *deezer.Track) {
	var on bool
	if s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM albums WHERE deezer_id = $1 AND $2 = ANY(tracklist))`, albumID, track.ID).Scan(&on) != nil || !on {
		return
	}
	if a := s.albumCard(albumID); a != nil {
		track.AlbumID, track.Album, track.CoverURL = a.ID, a.Title, a.CoverURL
	}
}

func (s *Server) albumVisibleTo(v viewer, id string) bool {
	if !isLocalID(id) {
		return true
	}
	_, err := s.catalogAlbum(v, id)
	return err == nil
}

func (s *Server) albumCard(id string) *deezer.Album {
	if isLocalID(id) {
		cards, err := s.localAlbumCards([]string{id})
		if err != nil || len(cards) == 0 {
			return nil
		}
		return &cards[0]
	}
	full, err := s.Deezer.GetAlbum(id)
	if err != nil {
		return nil
	}
	a := &deezer.Album{ID: full.ID, Title: full.Title, CoverURL: full.CoverURL, ArtistID: full.ArtistID, ArtistName: full.ArtistName}
	s.overlayFor(nil, []*deezer.Album{a}, nil)
	return a
}

func (s *Server) resolvePlayContext(r *http.Request, userID string, c playContext, track *deezer.Track) (playContext, []byte) {
	albumCtx := playContext{Kind: "album", ID: track.AlbumID}
	switch c.Kind {
	case "liked":
		return playContext{Kind: "liked", ID: ""}, nil
	case "album":
		if validCatalogID(c.ID) && s.albumVisibleTo(viewerOf(r), c.ID) {
			return c, nil
		}
	case "playlist":
		if _, err := s.visiblePlaylist(userID, c.ID); err == nil {
			return c, nil
		}
	case "mix":
		if m, ok := s.mixCard(r, userID, c.ID); ok {
			meta, _ := json.Marshal(m)
			return c, meta
		}
	case "artist":
		if !validCatalogID(c.ID) {
			break
		}
		var meta []byte
		err := s.DB.QueryRow(
			`SELECT context_meta FROM plays WHERE context_kind = 'artist' AND context_id = $1 AND context_meta IS NOT NULL ORDER BY id DESC LIMIT 1`,
			c.ID).Scan(&meta)
		if err == nil {
			return c, meta
		}
		artist, err := s.catalogArtist(c.ID)
		if err != nil {
			log.Printf("play context artist %s: %v", c.ID, err)
			break
		}
		meta, _ = json.Marshal(artist)
		return c, meta
	}
	return albumCtx, nil
}

type recentItem struct {
	Kind     string         `json:"kind"`
	Album    *deezer.Album  `json:"album,omitempty"`
	Artist   *deezer.Artist `json:"artist,omitempty"`
	Playlist *playlistInfo  `json:"playlist,omitempty"`
	Mix      *mix           `json:"mix,omitempty"`
}

const recentLimit = 12

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	rows, err := s.DB.Query(`
		SELECT context_kind, context_id, meta, context_meta FROM (
			SELECT DISTINCT ON (context_kind, context_id) context_kind, context_id, meta, context_meta, played_at
			  FROM plays
			 WHERE user_id = $1 AND played_at > now() - interval '90 days'
			 ORDER BY context_kind, context_id, played_at DESC) x
		 ORDER BY played_at DESC
		 LIMIT 30`, userID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	type row struct {
		kind, id      string
		meta, ctxMeta []byte
	}
	var found []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.kind, &x.id, &x.meta, &x.ctxMeta); err != nil {
			rows.Close()
			serverError(w, r, "scan failed", err)
			return
		}
		found = append(found, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		serverError(w, r, "query failed", err)
		return
	}

	items := []recentItem{}
	var albums []*deezer.Album
	var artists []*deezer.Artist
	for _, x := range found {
		if len(items) == recentLimit {
			break
		}
		switch x.kind {
		case "liked":
			items = append(items, recentItem{Kind: "liked"})
		case "album":
			var t deezer.Track
			if json.Unmarshal(x.meta, &t) != nil || x.id == "" {
				continue
			}
			if t.AlbumID != "" && t.AlbumID != x.id {
				if a := s.albumCard(x.id); a != nil {
					items = append(items, recentItem{Kind: "album", Album: a})
				}
				continue
			}
			if t.CoverURL == "" {
				if a := s.albumCard(x.id); a != nil {
					items = append(items, recentItem{Kind: "album", Album: a})
					continue
				}
			}
			a := &deezer.Album{ID: x.id, Title: t.Album, CoverURL: t.CoverURL, ArtistID: t.ArtistID, ArtistName: t.Artist}
			albums = append(albums, a)
			items = append(items, recentItem{Kind: "album", Album: a})
		case "artist":
			var a deezer.Artist
			if json.Unmarshal(x.ctxMeta, &a) != nil {
				continue
			}
			artists = append(artists, &a)
			items = append(items, recentItem{Kind: "artist", Artist: &a})
		case "playlist":
			p, err := s.visiblePlaylist(userID, x.id)
			if err != nil {
				continue
			}
			items = append(items, recentItem{Kind: "playlist", Playlist: &p})
		case "mix":
			var m mix
			if json.Unmarshal(x.ctxMeta, &m) == nil && m.ID != "" {
				items = append(items, recentItem{Kind: "mix", Mix: &m})
			}
		}
	}
	s.overlayFor(artists, albums, nil)
	if !showHidden(r) {
		items = slices.DeleteFunc(items, func(it recentItem) bool { return it.Album != nil && it.Album.Hidden })
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

const (
	suggestionLimit     = 12
	suggestionSeedLimit = 3
	suggestionSeedTries = 12
)

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	played, err := s.recentlyPlayedArtists(userID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	followed, err := s.followedArtists(userID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	known := map[string]bool{}
	for _, a := range slices.Concat(played, followed) {
		known[a.ID] = true
	}
	seeds, lists := s.suggestionSeeds(seedCandidates(played, followed), known)
	artists, used := interleaveSuggestions(seeds, lists)
	s.overlayFor(append(artistPtrs(artists), artistPtrs(used)...), nil, nil)
	basedOn := make([]string, len(used))
	for i, a := range used {
		basedOn[i] = a.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists, "based_on": basedOn})
}

func (s *Server) recentlyPlayedArtists(userID string) ([]deezer.Artist, error) {
	rows, err := s.DB.Query(`
		SELECT meta->>'artist_id', COALESCE(max(meta->>'artist'), '')
		  FROM plays
		 WHERE user_id = $1 AND played_at > now() - interval '30 days' AND COALESCE(meta->>'artist_id', '') NOT IN ('', '0')
		 GROUP BY 1 ORDER BY count(*) DESC, 2, 1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artists []deezer.Artist
	for rows.Next() {
		var a deezer.Artist
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func seedCandidates(played, followed []deezer.Artist) []deezer.Artist {
	var out []deezer.Artist
	seen := map[string]bool{}
	for _, a := range slices.Concat(played, followed) {
		if len(out) == suggestionSeedTries {
			break
		}
		if validDeezerID(a.ID) && !seen[a.ID] {
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	return out
}

func (s *Server) suggestionSeeds(candidates []deezer.Artist, known map[string]bool) (seeds []deezer.Artist, lists [][]deezer.Artist) {
	for start := 0; start < len(candidates) && len(seeds) < suggestionSeedLimit; start += suggestionSeedLimit {
		batch := candidates[start:min(start+suggestionSeedLimit, len(candidates))]
		for i, related := range s.relatedOfEach(batch) {
			fresh := slices.DeleteFunc(slices.Clone(related), func(a deezer.Artist) bool { return known[a.ID] })
			if len(fresh) > 0 && len(seeds) < suggestionSeedLimit {
				seeds = append(seeds, batch[i])
				lists = append(lists, fresh)
			}
		}
	}
	return seeds, lists
}

func (s *Server) relatedOfEach(artists []deezer.Artist) [][]deezer.Artist {
	lists := make([][]deezer.Artist, len(artists))
	var wg sync.WaitGroup
	for i, a := range artists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := guard("suggestions related", func() (err error) { lists[i], err = s.relatedArtists(a.ID); return })
			if err != nil {
				log.Printf("suggestions: related to %s: %v", a.ID, err)
			}
		}()
	}
	wg.Wait()
	return lists
}

func interleaveSuggestions(seeds []deezer.Artist, lists [][]deezer.Artist) (artists, used []deezer.Artist) {
	artists = []deezer.Artist{}
	taken := map[string]bool{}
	gave := make([]bool, len(lists))
	for round := 0; len(artists) < suggestionLimit; round++ {
		more := false
		for i, l := range lists {
			if round >= len(l) {
				continue
			}
			more = true
			if a := l[round]; !taken[a.ID] && len(artists) < suggestionLimit {
				taken[a.ID] = true
				artists = append(artists, a)
				gave[i] = true
			}
		}
		if !more {
			break
		}
	}
	for i, seed := range seeds {
		if gave[i] {
			used = append(used, seed)
		}
	}
	return artists, used
}

func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	var artist *deezer.Artist
	var err error
	if isLocalID(id) {
		artist, err = s.catalogArtist(id)
	} else {
		artist, err = s.Deezer.GetArtist(id)
	}
	if err != nil {
		writeCatalogError(w, "artist lookup failed", err)
		return
	}
	meta, _ := json.Marshal(artist)
	if _, err := s.DB.Exec(
		`INSERT INTO followed_artists (user_id, deezer_id, meta) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		userID, id, meta,
	); err != nil {
		serverError(w, r, "could not follow the artist", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	if _, err := s.DB.Exec(`DELETE FROM followed_artists WHERE user_id = $1 AND deezer_id = $2`, userID, r.PathValue("id")); err != nil {
		serverError(w, r, "could not unfollow the artist", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) followedArtists(userID string) ([]deezer.Artist, error) {
	rows, err := s.DB.Query(`SELECT meta FROM followed_artists WHERE user_id = $1 ORDER BY added_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artists := []deezer.Artist{}
	for rows.Next() {
		var meta []byte
		var a deezer.Artist
		if rows.Scan(&meta) == nil && json.Unmarshal(meta, &a) == nil {
			artists = append(artists, a)
		}
	}
	return artists, rows.Err()
}

func (s *Server) handleFollowedArtists(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	artists, err := s.followedArtists(userID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	s.overlayFor(artistPtrs(artists), nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists})
}

const (
	releasesWindow = 30 * 24 * time.Hour
	releasesLimit  = 20
	releasesTTL    = 6 * time.Hour
)

func (s *Server) handleNewReleases(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	artists, err := s.followedArtists(userID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	since := time.Now().Add(-releasesWindow).Format("2006-01-02")

	lists := make([][]deezer.Album, len(artists))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, artist := range artists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := guard("new releases", func() error {
				var list []deezer.Album
				if !isLocalID(artist.ID) {
					var err error
					list, err = storedCached(s, "albums:"+artist.ID, releasesTTL, func() ([]deezer.Album, error) {
						return s.Deezer.GetArtistAlbums(artist.ID, artistAlbumsFirstPage)
					})
					if err != nil {
						return err
					}
				}
				lists[i] = s.creditedDiscography(artist.ID, cloneAlbums(list))
				return nil
			})
			if err != nil {
				log.Printf("new releases: albums of %s: %v", artist.ID, err)
			}
		}()
	}
	wg.Wait()

	albums := []deezer.Album{}
	followedOn := map[string][]deezer.ArtistRef{}
	for i, list := range lists {
		artist := artists[i]
		for _, a := range list {
			if a.ReleaseDate < since {
				continue
			}
			refs := followedOn[a.ID]
			if len(refs) == 0 {
				if a.ArtistName == "" {
					a.ArtistName = artist.Name
				}
				albums = append(albums, a)
			} else if refs[len(refs)-1].ID == artist.ID {
				continue
			}
			followedOn[a.ID] = append(refs, deezer.ArtistRef{ID: artist.ID, Name: artist.Name, PictureURL: artist.PictureURL})
		}
	}

	sort.SliceStable(albums, func(i, j int) bool {
		if albums[i].ReleaseDate != albums[j].ReleaseDate {
			return albums[i].ReleaseDate > albums[j].ReleaseDate
		}
		return albums[i].ID > albums[j].ID
	})
	if len(albums) > releasesLimit {
		albums = albums[:releasesLimit]
	}
	s.creditReleases(albums, followedOn)
	s.overlayFor(nil, albumPtrs(albums), nil)
	writeJSON(w, http.StatusOK, map[string]any{"albums": visibleAlbums(r, albums)})
}

func (s *Server) creditReleases(albums []deezer.Album, followedOn map[string][]deezer.ArtistRef) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range albums {
		a := &albums[i]
		credits := mainCredits(*a)
		followed := followedOn[a.ID]
		if len(followed) < 2 {
			if len(credits) > 0 {
				a.Contributors = credits
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if len(credits) == 0 && !isLocalID(a.ID) {
				sem <- struct{}{}
				err := guard("new releases: credits", func() error {
					full, err := s.Deezer.GetAlbum(a.ID)
					if err == nil {
						credits = mainCredits(*full)
					}
					return err
				})
				<-sem
				if err != nil {
					log.Printf("new releases: credits of %s: %v", a.ID, err)
				}
			}
			for _, f := range followed {
				if !slices.ContainsFunc(credits, func(c deezer.ArtistRef) bool { return c.ID == f.ID }) {
					credits = append(credits, f)
				}
			}
			a.Contributors, a.ArtistID, a.ArtistName = credits, credits[0].ID, credits[0].Name
		}()
	}
	wg.Wait()
}

func mainCredits(a deezer.Album) []deezer.ArtistRef {
	var out []deezer.ArtistRef
	for _, c := range a.Contributors {
		if slices.Contains(a.MainArtistIDs, c.ID) {
			out = append(out, c)
		}
	}
	return out
}

const artistAlbumsFirstPage = 100

func (s *Server) handleLibraryTimes(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	times := func(query string) (map[string]time.Time, error) {
		rows, err := s.DB.Query(query, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]time.Time{}
		for rows.Next() {
			var key string
			var at time.Time
			if err := rows.Scan(&key, &at); err != nil {
				return nil, err
			}
			out[key] = at
		}
		return out, rows.Err()
	}
	added, err := times(`
		SELECT 'album:' || deezer_id, added_at FROM saved_albums WHERE user_id = $1
		UNION ALL SELECT 'artist:' || deezer_id, added_at FROM followed_artists WHERE user_id = $1
		UNION ALL SELECT 'playlist:' || id, created_at FROM playlists WHERE user_id = $1`)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	played, err := times(`SELECT context_kind || ':' || context_id, max(played_at) FROM plays WHERE user_id = $1 GROUP BY 1`)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "played": played})
}
