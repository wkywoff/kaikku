package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
	"kaikku/internal/lastfm"
	"kaikku/internal/safe"
)

const lastfmMaxAge = 14 * 24 * time.Hour

type lastfmLink struct {
	username, sessionKey string
	scrobble, love       bool
}

func (s *Server) lastfmLinkOf(userID string) *lastfmLink {
	if s.Lastfm == nil {
		return nil
	}
	var l lastfmLink
	err := s.DB.QueryRow(`SELECT username, session_key, scrobble, love FROM lastfm_links WHERE user_id = $1`, userID).
		Scan(&l.username, &l.sessionKey, &l.scrobble, &l.love)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("lastfm link of %s: %v", userID, err)
		}
		return nil
	}
	return &l
}

func (s *Server) dropLastfmLink(userID string) {
	if _, err := s.DB.Exec(`DELETE FROM lastfm_links WHERE user_id = $1`, userID); err != nil {
		log.Printf("lastfm unlink %s: %v", userID, err)
	}
}

func lastfmTrack(t *deezer.Track) lastfm.Track {
	return lastfm.Track{Artist: t.Artist, Title: t.Title, Album: t.Album, Duration: (t.DurationMs + 500) / 1000}
}

func (s *Server) handleLastfmStatus(w http.ResponseWriter, r *http.Request) {
	if s.Lastfm == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	out := map[string]any{"available": true, "auth_url": s.Lastfm.AuthURL(r.URL.Query().Get("cb"))}
	if l := s.lastfmLinkOf(currentUser(r)); l != nil {
		var pending int
		s.DB.QueryRow(`SELECT count(*) FROM lastfm_queue WHERE user_id = $1`, currentUser(r)).Scan(&pending)
		out["username"], out["scrobble"], out["love"], out["pending"] = l.username, l.scrobble, l.love, pending
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLastfmLink(w http.ResponseWriter, r *http.Request) {
	if s.Lastfm == nil {
		writeError(w, http.StatusNotFound, "last.fm is not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || len(req.Token) > 128 {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}
	name, key, err := s.Lastfm.Session(r.Context(), req.Token)
	var lfErr *lastfm.Error
	if errors.As(err, &lfErr) && !lastfm.Retryable(err) {
		writeError(w, http.StatusBadRequest, "last.fm did not accept the token")
		return
	}
	if err != nil {
		log.Printf("lastfm session: %v", err)
		writeError(w, http.StatusBadGateway, "last.fm is unavailable")
		return
	}
	if _, err := s.DB.Exec(
		`INSERT INTO lastfm_links (user_id, username, session_key) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET username = EXCLUDED.username, session_key = EXCLUDED.session_key, linked_at = now()`,
		currentUser(r), name, key,
	); err != nil {
		serverError(w, r, "could not save the link", err)
		return
	}
	s.handleLastfmStatus(w, r)
}

func (s *Server) handleLastfmSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scrobble *bool `json:"scrobble"`
		Love     *bool `json:"love"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := s.DB.Exec(
		`UPDATE lastfm_links SET scrobble = COALESCE($2, scrobble), love = COALESCE($3, love) WHERE user_id = $1`,
		currentUser(r), req.Scrobble, req.Love)
	if err != nil {
		serverError(w, r, "could not save", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "last.fm is not linked")
		return
	}
	s.handleLastfmStatus(w, r)
}

func (s *Server) handleLastfmUnlink(w http.ResponseWriter, r *http.Request) {
	if _, err := s.DB.Exec(`DELETE FROM lastfm_links WHERE user_id = $1`, currentUser(r)); err != nil {
		serverError(w, r, "could not unlink", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) lastfmTrackFor(w http.ResponseWriter, r *http.Request, id string) (*lastfmLink, *deezer.Track, bool) {
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "deezer_id must be a song ID")
		return nil, nil, false
	}
	l := s.lastfmLinkOf(currentUser(r))
	if l == nil || !l.scrobble {
		w.WriteHeader(http.StatusNoContent)
		return nil, nil, false
	}
	track, err := s.catalogTrack(viewerOf(r), id)
	if err != nil {
		writeCatalogError(w, "track lookup failed", err)
		return nil, nil, false
	}
	return l, track, true
}

func (s *Server) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeezerID string `json:"deezer_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	l, track, ok := s.lastfmTrackFor(w, r, req.DeezerID)
	if !ok {
		return
	}
	userID := currentUser(r)
	s.inBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := s.Lastfm.NowPlaying(ctx, l.sessionKey, lastfmTrack(track))
		if lastfm.SessionInvalid(err) {
			s.dropLastfmLink(userID)
		} else if err != nil {
			log.Printf("lastfm now playing: %v", err)
		}
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleScrobble(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeezerID  string `json:"deezer_id"`
		StartedAt int64  `json:"started_at"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	started := time.Unix(req.StartedAt, 0)
	if age := time.Since(started); req.StartedAt <= 0 || age < -time.Minute || age > lastfmMaxAge {
		writeError(w, http.StatusBadRequest, "started_at out of range")
		return
	}
	_, track, ok := s.lastfmTrackFor(w, r, req.DeezerID)
	if !ok {
		return
	}
	lt := lastfmTrack(track)
	if _, err := s.DB.Exec(
		`INSERT INTO lastfm_queue (user_id, artist, track, album, duration, played_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		currentUser(r), lt.Artist, lt.Title, lt.Album, lt.Duration, started,
	); err != nil {
		serverError(w, r, "could not queue the scrobble", err)
		return
	}
	select {
	case s.lastfmWake <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) lastfmLove(userID string, track *deezer.Track, on bool) {
	l := s.lastfmLinkOf(userID)
	if l == nil || !l.love || track == nil {
		return
	}
	ts := []deezer.Track{*track}
	s.overlayFor(nil, nil, ts)
	s.inBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := s.Lastfm.Love(ctx, l.sessionKey, lastfmTrack(&ts[0]), on)
		if lastfm.SessionInvalid(err) {
			s.dropLastfmLink(userID)
		} else if err != nil {
			log.Printf("lastfm love: %v", err)
		}
	})
}

func (s *Server) StartLastfm(ctx context.Context) {
	if s.Lastfm == nil {
		return
	}
	s.lastfmWake = make(chan struct{}, 1)
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			safe.Run("lastfm", func() { s.flushLastfm(ctx) })
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			case <-s.lastfmWake:
			}
		}
	}()
}

func (s *Server) flushLastfm(ctx context.Context) {
	if _, err := s.DB.Exec(`DELETE FROM lastfm_queue WHERE played_at < now() - interval '14 days'`); err != nil {
		log.Printf("lastfm queue cleanup: %v", err)
	}
	rows, err := s.DB.Query(
		`SELECT DISTINCT q.user_id, l.session_key FROM lastfm_queue q JOIN lastfm_links l USING (user_id) WHERE q.next_try <= now()`)
	if err != nil {
		log.Printf("lastfm queue: %v", err)
		return
	}
	type due struct{ userID, sk string }
	var users []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.userID, &d.sk) == nil {
			users = append(users, d)
		}
	}
	closeRows("lastfm queue", rows)
	for _, u := range users {
		for ctx.Err() == nil && s.flushLastfmBatch(ctx, u.userID, u.sk) {
		}
	}
}

func (s *Server) flushLastfmBatch(ctx context.Context, userID, sk string) bool {
	rows, err := s.DB.Query(
		`SELECT id, artist, track, album, duration, played_at FROM lastfm_queue
		  WHERE user_id = $1 AND next_try <= now() ORDER BY played_at LIMIT $2`, userID, lastfm.MaxBatch)
	if err != nil {
		log.Printf("lastfm batch: %v", err)
		return false
	}
	var ids []int64
	var batch []lastfm.Track
	for rows.Next() {
		var id int64
		var t lastfm.Track
		if rows.Scan(&id, &t.Artist, &t.Title, &t.Album, &t.Duration, &t.Started) == nil {
			ids, batch = append(ids, id), append(batch, t)
		}
	}
	closeRows("lastfm batch", rows)
	if len(batch) == 0 {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.Lastfm.Scrobble(cctx, sk, batch)
	cancel()
	switch {
	case lastfm.SessionInvalid(err):
		s.dropLastfmLink(userID)
		return false
	case lastfm.Retryable(err):
		log.Printf("lastfm scrobble (will retry): %v", err)
		s.DB.Exec(
			`UPDATE lastfm_queue SET attempts = attempts + 1,
			        next_try = now() + least(interval '1 minute' * power(2, least(attempts, 9)), interval '6 hours')
			  WHERE id = ANY($1)`, pq.Array(ids))
		return false
	case err != nil:
		log.Printf("lastfm scrobble rejected: %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM lastfm_queue WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		log.Printf("lastfm queue delete: %v", err)
		return false
	}
	return len(batch) == lastfm.MaxBatch
}
