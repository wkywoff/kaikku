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

// Last.fm (миграция 0023): человек привязывает аккаунт в настройках, дальше
// его прослушивания уходят скробблами, текущий трек — «сейчас играет», а
// лайки — ♥. Без ключа приложения (LASTFM_API_KEY/SECRET) всё выключено.
//
// Скроббл сначала ложится в lastfm_queue и отправляется сразу; если Last.fm
// недоступен — повтор с нарастающей паузой (StartLastfm), так что
// прослушивания не теряются.

const lastfmMaxAge = 14 * 24 * time.Hour // старше Last.fm не принимает (и flushLastfm)

type lastfmLink struct {
	username, sessionKey string
	scrobble, love       bool
}

// lastfmLinkOf — привязка человека; nil — не привязан или Last.fm выключен.
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

// dropLastfmLink — Last.fm сказал, что ключ отозван: привязки больше нет
// (очередь человека уходит вместе с ней).
func (s *Server) dropLastfmLink(userID string) {
	if _, err := s.DB.Exec(`DELETE FROM lastfm_links WHERE user_id = $1`, userID); err != nil {
		log.Printf("lastfm unlink %s: %v", userID, err)
	}
}

func lastfmTrack(t *deezer.Track) lastfm.Track {
	return lastfm.Track{Artist: t.Artist, Title: t.Title, Album: t.Album, Duration: (t.DurationMs + 500) / 1000}
}

// GET /api/me/lastfm — доступен ли Last.fm и привязан ли аккаунт.
func (s *Server) handleLastfmStatus(w http.ResponseWriter, r *http.Request) {
	if s.Lastfm == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	// cb — куда Last.fm вернёт человека с токеном (страница /lastfm сайта).
	out := map[string]any{"available": true, "auth_url": s.Lastfm.AuthURL(r.URL.Query().Get("cb"))}
	if l := s.lastfmLinkOf(currentUser(r)); l != nil {
		var pending int
		s.DB.QueryRow(`SELECT count(*) FROM lastfm_queue WHERE user_id = $1`, currentUser(r)).Scan(&pending)
		out["username"], out["scrobble"], out["love"], out["pending"] = l.username, l.scrobble, l.love, pending
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/me/lastfm {token} — токен из веб-авторизации Last.fm меняется
// на ключ сессии; повторная привязка (другой аккаунт) заменяет прежнюю.
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
		// 4, 14, 15: токен неверный, не подтверждён или устарел
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
		writeError(w, http.StatusInternalServerError, "could not save the link")
		return
	}
	s.handleLastfmStatus(w, r)
}

// PATCH /api/me/lastfm {scrobble?, love?}
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
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "last.fm is not linked")
		return
	}
	s.handleLastfmStatus(w, r)
}

// DELETE /api/me/lastfm — отвязать (неотправленные скробблы пропадают).
func (s *Server) handleLastfmUnlink(w http.ResponseWriter, r *http.Request) {
	if _, err := s.DB.Exec(`DELETE FROM lastfm_links WHERE user_id = $1`, currentUser(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unlink")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// lastfmTrackFor — трек запроса {deezer_id} с правками, если человек
// привязал Last.fm и скробблит; иначе nil (запрос молча ничего не делает).
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

// POST /api/me/now-playing {deezer_id} — трек начал играть.
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

// POST /api/me/scrobbles {deezer_id, started_at} — трек дослушан до порога
// Last.fm (половина или 4 минуты); started_at — начало, unix-секунды.
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
		writeError(w, http.StatusInternalServerError, "could not queue the scrobble")
		return
	}
	select {
	case s.lastfmWake <- struct{}{}:
	default: // отправитель уже разбужен (или не запущен — тесты)
	}
	w.WriteHeader(http.StatusNoContent)
}

// lastfmLove — лайк или его снятие в kaikku ставит или снимает ♥ на Last.fm.
func (s *Server) lastfmLove(userID string, track *deezer.Track, on bool) {
	l := s.lastfmLinkOf(userID)
	if l == nil || !l.love || track == nil {
		return
	}
	// Снимок лайка — как у Deezer: название и исполнитель — с правками админа.
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

// StartLastfm запускает отправку очереди скробблов: сразу после нового
// скроббла и раз в минуту — то, чему пришло время повтора.
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

// flushLastfm отправляет всё, чему пришло время, пачками по 50 на человека.
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
	rows.Close()
	for _, u := range users {
		for ctx.Err() == nil && s.flushLastfmBatch(ctx, u.userID, u.sk) {
		}
	}
}

// flushLastfmBatch отправляет одну пачку человека; true — отправилась и,
// может быть, есть ещё.
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
	rows.Close()
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
		// Неверные данные — повтор не поможет.
		log.Printf("lastfm scrobble rejected: %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM lastfm_queue WHERE id = ANY($1)`, pq.Array(ids)); err != nil {
		log.Printf("lastfm queue delete: %v", err)
		return false
	}
	return len(batch) == lastfm.MaxBatch
}
