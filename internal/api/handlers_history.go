package api

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// История прослушиваний и подписки на артистов (миграция 0011). Из них
// главная строит «Недавно прослушанное», «Тебе может понравиться» и
// «Новые релизы».

// --- история ---

type playContext struct {
	Kind string `json:"kind"` // album | playlist | artist | liked; пусто — альбом трека
	ID   string `json:"id"`
}

// handleRecordPlay — клиент присылает трек, дослушанный до 30 секунд (или до
// половины), и откуда его включили.
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
	// Снимок — с правками админа: прослушивание засчитывается исполнителю,
	// которому релиз переназначен, а не дубликату у Deezer.
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
	if _, err := s.DB.Exec(
		`INSERT INTO plays (user_id, deezer_id, meta, context_kind, context_id, context_meta) VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, req.DeezerID, meta, ctx.Kind, ctx.ID, ctxMeta,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record the play")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// asTracklistOf: a track from another release put on album albumID's
// tracklist (tracklist.go) and played there counts for that album — its
// title, cover and ID in the snapshot, as on the album page — in the recent
// list and the year in review.
func (s *Server) asTracklistOf(albumID string, track *deezer.Track) {
	var on bool
	if s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM albums WHERE deezer_id = $1 AND $2 = ANY(tracklist))`, albumID, track.ID).Scan(&on) != nil || !on {
		return
	}
	if a := s.albumCard(albumID); a != nil {
		track.AlbumID, track.Album, track.CoverURL = a.ID, a.Title, a.CoverURL
	}
}

// albumCard — the album's card fields (title, cover, artist) with the
// admin's edits; nil if Deezer doesn't answer.
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
	// Без Contributors: overlay правит их на месте, а срез общий с кешем.
	a := &deezer.Album{ID: full.ID, Title: full.Title, CoverURL: full.CoverURL, ArtistID: full.ArtistID, ArtistName: full.ArtistName}
	s.overlayFor(nil, []*deezer.Album{a}, nil)
	return a
}

// resolvePlayContext checks the context the client named; anything unknown
// or not visible to the user falls back to the track's own album. For an
// artist it also returns their snapshot (name, picture) for the recent list,
// for a mix — its card (mixes.go).
func (s *Server) resolvePlayContext(r *http.Request, userID string, c playContext, track *deezer.Track) (playContext, []byte) {
	albumCtx := playContext{Kind: "album", ID: track.AlbumID}
	switch c.Kind {
	case "liked":
		return playContext{Kind: "liked", ID: ""}, nil
	case "album":
		if validCatalogID(c.ID) {
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

// recentItem — карточка «Недавно прослушанного»: ровно одно из полей.
type recentItem struct {
	Kind     string         `json:"kind"`
	Album    *deezer.Album  `json:"album,omitempty"`
	Artist   *deezer.Artist `json:"artist,omitempty"`
	Playlist *playlistInfo  `json:"playlist,omitempty"`
	Mix      *mix           `json:"mix,omitempty"`
}

const recentLimit = 12

// handleRecent — откуда человек недавно слушал: альбомы, плейлисты,
// артисты, «Любимые» — последние сверху, без повторов.
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
		writeError(w, http.StatusInternalServerError, "query failed")
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
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		found = append(found, x)
	}
	rows.Close()

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
				// Трек с другого релиза в треклисте этого (tracklist.go), записанный
				// до asTracklistOf: карточка — по самому альбому, не по треку.
				if a := s.albumCard(x.id); a != nil {
					items = append(items, recentItem{Kind: "album", Album: a})
				}
				continue
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
				continue // удалён или стал приватным
			}
			items = append(items, recentItem{Kind: "playlist", Playlist: &p})
		case "mix":
			// Карточка — какой микс был тогда; если его уже нет, страница
			// микса так и скажет.
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

// handleSuggestions — «Тебе может понравиться»: похожие на артистов, которых
// человек больше всего слушал за месяц, кроме тех, на кого он подписан.
func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	rows, err := s.DB.Query(`
		SELECT meta->>'artist_id', max(meta->>'artist'), count(*)
		  FROM plays
		 WHERE user_id = $1 AND played_at > now() - interval '30 days' AND COALESCE(meta->>'artist_id', '') NOT IN ('', '0')
		 GROUP BY 1 ORDER BY 3 DESC, 2 LIMIT 3`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var topIDs, basedOn []string
	for rows.Next() {
		var id, name string
		var n int
		if rows.Scan(&id, &name, &n) == nil {
			topIDs = append(topIDs, id)
			basedOn = append(basedOn, name)
		}
	}
	rows.Close()

	skip := map[string]bool{}
	followed, err := s.idList(`SELECT deezer_id FROM followed_artists WHERE user_id = $1`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	for _, id := range append(followed, topIDs...) {
		skip[id] = true
	}

	// По очереди от каждого из топа, чтобы первый не занял весь ряд.
	var lists [][]deezer.Artist
	for _, id := range topIDs {
		if isLocalID(id) {
			continue // похожих на своего артиста Deezer не знает
		}
		related, err := s.relatedArtists(id)
		if err != nil {
			log.Printf("suggestions: related to %s: %v", id, err)
			continue
		}
		lists = append(lists, related)
	}
	artists := []deezer.Artist{}
	for i := 0; len(artists) < 12; i++ {
		more := false
		for _, l := range lists {
			if i >= len(l) {
				continue
			}
			more = true
			if a := l[i]; !skip[a.ID] && len(artists) < 12 {
				skip[a.ID] = true
				artists = append(artists, a)
			}
		}
		if !more {
			break
		}
	}
	// «Похоже на …»: имена — из истории прослушиваний, какими были тогда;
	// правки админа (новое имя) накладываются так же, как на карточки.
	based := make([]deezer.Artist, len(topIDs))
	for i := range topIDs {
		based[i] = deezer.Artist{ID: topIDs[i], Name: basedOn[i]}
	}
	s.overlayFor(append(artistPtrs(artists), artistPtrs(based)...), nil, nil)
	for i := range based {
		basedOn[i] = based[i].Name
	}
	if basedOn == nil {
		basedOn = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists, "based_on": basedOn})
}

// --- подписки на артистов ---

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
		writeError(w, http.StatusInternalServerError, "could not follow the artist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	if _, err := s.DB.Exec(`DELETE FROM followed_artists WHERE user_id = $1 AND deezer_id = $2`, userID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unfollow the artist")
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
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.overlayFor(artistPtrs(artists), nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"artists": artists})
}

// Новые релизы — вышедшие за последние releasesWindow у артистов, на
// которых человек подписан. Дискографии кешируются, так что главная не
// ходит в Deezer за каждым артистом при каждом открытии.
const (
	releasesWindow = 90 * 24 * time.Hour
	releasesLimit  = 20
	releasesTTL    = 6 * time.Hour
)

func (s *Server) handleNewReleases(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	artists, err := s.followedArtists(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	since := time.Now().Add(-releasesWindow).Format("2006-01-02")

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // Deezer пускает ~50 запросов за 5 с
	seen := map[string]bool{}
	albums := []deezer.Album{}
	for _, artist := range artists {
		wg.Add(1)
		go func(artist deezer.Artist) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var list []deezer.Album
			err := guard("new releases", func() error {
				if !isLocalID(artist.ID) { // у своего артиста все релизы — из базы (creditedDiscography)
					var err error
					list, err = storedCached(s, "albums:"+artist.ID, releasesTTL, func() ([]deezer.Album, error) {
						return s.Deezer.GetArtistAlbums(artist.ID, artistAlbumsFirstPage)
					})
					if err != nil {
						return err
					}
				}
				// Правки накладываются на месте — не в кеш; релизы, переназначенные
				// артисту, — тоже его новинки.
				list = s.creditedDiscography(artist.ID, cloneAlbums(list))
				return nil
			})
			if err != nil {
				log.Printf("new releases: albums of %s: %v", artist.ID, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, a := range list {
				if a.ReleaseDate < since || seen[a.ID] {
					continue
				}
				seen[a.ID] = true
				if a.ArtistName == "" {
					a.ArtistName = artist.Name
				}
				albums = append(albums, a)
			}
		}(artist)
	}
	wg.Wait()

	sort.SliceStable(albums, func(i, j int) bool {
		if albums[i].ReleaseDate != albums[j].ReleaseDate {
			return albums[i].ReleaseDate > albums[j].ReleaseDate
		}
		return albums[i].ID > albums[j].ID
	})
	if len(albums) > releasesLimit {
		albums = albums[:releasesLimit]
	}
	s.overlayFor(nil, albumPtrs(albums), nil)
	writeJSON(w, http.StatusOK, map[string]any{"albums": visibleAlbums(r, albums)})
}

// Новинки всегда на первой странице дискографии (Deezer отдаёт свежие первыми).
const artistAlbumsFirstPage = 100

// GET /api/me/library/times — для сортировки медиатеки: когда что
// добавлено (сохранённые альбомы, подписки, свои плейлисты) и когда
// последний раз слушалось (по контексту прослушиваний). Ключи — "<kind>:<id>",
// kind — album | artist | playlist (и liked у прослушиваний).
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
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	played, err := times(`SELECT context_kind || ':' || context_id, max(played_at) FROM plays WHERE user_id = $1 GROUP BY 1`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "played": played})
}
