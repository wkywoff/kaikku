package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// Профили и подписки между людьми (миграция 0035): страница /user/<ник> с
// публичными плейлистами. Взаимная подписка — «друзья»: только их владелец
// зовёт в соавторы плейлиста (playlist_collab.go). Списка всех людей нет —
// профиль находят по ссылке, по нику у плейлиста или по нику целиком в
// поиске.

// person — человек глазами текущего пользователя.
type person struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	IFollow   bool   `json:"i_follow"`
	FollowsMe bool   `json:"follows_me"`
}

// personSelect — $1 — текущий пользователь.
const personSelect = `
	SELECT u.id, u.username,
	       EXISTS (SELECT 1 FROM user_follows f WHERE f.follower_id = $1 AND f.followee_id = u.id),
	       EXISTS (SELECT 1 FROM user_follows f WHERE f.follower_id = u.id AND f.followee_id = $1)
	  FROM users u`

func scanPeople(rows *sql.Rows) ([]person, error) {
	defer rows.Close()
	out := []person{}
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.ID, &p.Username, &p.IFollow, &p.FollowsMe); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// userByName — человек по нику (без учёта регистра).
func (s *Server) userByName(me, name string) (person, error) {
	var p person
	err := s.DB.QueryRow(personSelect+` WHERE lower(u.username) = lower($2) ORDER BY u.username = $2 DESC LIMIT 1`, me, name).
		Scan(&p.ID, &p.Username, &p.IFollow, &p.FollowsMe)
	return p, err
}

// GET /api/users/{name} — профиль: человек, его публичные плейлисты; свой —
// ещё и люди вокруг (друзья, подписчики, подписки).
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	p, err := s.userByName(me, r.PathValue("name"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	rows, err := s.DB.Query(playlistSelect+` WHERE p.user_id = $2 AND p.is_public ORDER BY p.updated_at DESC`, me, p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	playlists := []playlistInfo{}
	for rows.Next() {
		pl, err := scanPlaylist(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		playlists = append(playlists, pl)
	}
	resp := map[string]any{"user": p, "me": p.ID == me, "playlists": playlists}
	if p.ID == me {
		people, err := s.people(me)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		resp["people"] = people
	}
	writeJSON(w, http.StatusOK, resp)
}

// people — на кого подписан и кто подписан на тебя.
func (s *Server) people(me string) ([]person, error) {
	rows, err := s.DB.Query(personSelect+`
		 WHERE u.id IN (SELECT followee_id FROM user_follows WHERE follower_id = $1
		                UNION SELECT follower_id FROM user_follows WHERE followee_id = $1)
		 ORDER BY lower(u.username)`, me)
	if err != nil {
		return nil, err
	}
	return scanPeople(rows)
}

// GET /api/me/people — люди вокруг: друзья (подписки в обе стороны),
// подписчики и подписки. Окно плейлиста берёт отсюда друзей.
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.people(currentUser(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

// PUT /api/users/{id}/follow — подписаться; DELETE — отписаться. Ответ —
// человек с новым состоянием.
func (s *Server) handleFollowUser(w http.ResponseWriter, r *http.Request) {
	me, other := currentUser(r), r.PathValue("id")
	if other == me {
		writeError(w, http.StatusBadRequest, "you cannot follow yourself")
		return
	}
	if _, err := s.DB.Exec(
		`INSERT INTO user_follows (follower_id, followee_id)
		 SELECT $1, u.id FROM users u WHERE u.id::text = $2
		 ON CONFLICT DO NOTHING`, me, other); err != nil {
		writeError(w, http.StatusInternalServerError, "could not follow")
		return
	}
	s.writePerson(w, me, other)
}

func (s *Server) handleUnfollowUser(w http.ResponseWriter, r *http.Request) {
	me, other := currentUser(r), r.PathValue("id")
	if _, err := s.DB.Exec(`DELETE FROM user_follows WHERE follower_id = $1 AND followee_id::text = $2`, me, other); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unfollow")
		return
	}
	s.writePerson(w, me, other)
}

func (s *Server) writePerson(w http.ResponseWriter, me, id string) {
	var p person
	err := s.DB.QueryRow(personSelect+` WHERE u.id::text = $2`, me, id).Scan(&p.ID, &p.Username, &p.IFollow, &p.FollowsMe)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// friends — подписаны друг на друга.
func (s *Server) friends(a, b string) bool {
	var ok bool
	s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM user_follows WHERE follower_id::text = $1 AND followee_id::text = $2)
	                  AND EXISTS (SELECT 1 FROM user_follows WHERE follower_id::text = $2 AND followee_id::text = $1)`, a, b).Scan(&ok)
	return ok
}

// searchPeople — человек для поиска: только по нику целиком, чтобы по
// первым буквам нельзя было перебрать всех.
func (s *Server) searchPeople(me, q string) []person {
	q = strings.TrimPrefix(strings.TrimSpace(q), "@")
	if q == "" {
		return []person{}
	}
	p, err := s.userByName(me, q)
	if err != nil || p.ID == me {
		return []person{}
	}
	return []person{p}
}
