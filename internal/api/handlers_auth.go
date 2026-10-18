package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"kaikku/internal/auth"
)

const (
	tokenTTL  = 30 * 24 * time.Hour
	inviteTTL = 7 * 24 * time.Hour

	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
)

// Токен старше этого продлевается сам (authed, X-Auth-Token): вход
// кончается только после tokenTTL без единого захода.
const tokenRenewAfter = 24 * time.Hour

// validUsername: 3–32 letters, digits, "_", "-", ".". The name is shown to
// other users (public playlists), so no spaces, control or invisible
// characters and no kilometre-long strings.
func validUsername(name string) bool {
	if n := utf8.RuneCountInString(name); n < minUsernameLen || n > maxUsernameLen {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func writeBusy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	writeError(w, http.StatusServiceUnavailable, "server is busy, try again in a few seconds")
}

var errInvite = errors.New("invite invalid")

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Invite   string `json:"invite"`
}

// handleRegister creates a user. Registration is invite-only: the code must
// exist, be unused and not expired, and is consumed in the same transaction.
// The only exception is an empty users table — that first user registers
// without an invite and becomes the admin.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Invite = strings.TrimSpace(req.Invite)
	if !validUsername(req.Username) || len(req.Password) < minPasswordLen || len(req.Password) > auth.MaxPasswordLen {
		writeError(w, http.StatusBadRequest, "username: 3–32 letters, digits, _ - . ; password: 8–256 characters")
		return
	}

	// Приглашение — до хеширования: хеш стоит ~0.2 с процессора, а запрос
	// без приглашения не должен стоить ничего. createUser проверяет его ещё
	// раз в транзакции.
	if ok, err := s.inviteUsable(req.Invite); err != nil {
		writeError(w, http.StatusInternalServerError, "could not check the invite")
		return
	} else if !ok {
		writeError(w, http.StatusForbidden, "registration requires a valid invite code")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if errors.Is(err, auth.ErrBusy) {
		writeBusy(w)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	id, err := s.createUser(req.Username, hash, req.Invite)
	switch {
	case errors.Is(err, errInvite):
		writeError(w, http.StatusForbidden, "registration requires a valid invite code")
		return
	case isUniqueViolation(err):
		writeError(w, http.StatusConflict, "username already taken")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}

	token, err := auth.IssueToken(id, tokenTTL, s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "token": token})
}

// handleRegisterInfo (GET /api/register): нужно ли приглашение. На пустой
// базе — нет: форма входа сразу открывает регистрацию первого (админа).
func (s *Server) handleRegisterInfo(w http.ResponseWriter, r *http.Request) {
	var first bool
	if err := s.DB.QueryRow(`SELECT NOT EXISTS (SELECT 1 FROM users)`).Scan(&first); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"first_user": first})
}

// inviteUsable: nobody has registered yet (the first user needs no invite)
// or the code is unused and not expired.
func (s *Server) inviteUsable(code string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(
		`SELECT NOT EXISTS (SELECT 1 FROM users)
		     OR EXISTS (SELECT 1 FROM invites WHERE code = $1 AND used_by IS NULL AND expires_at > now())`, code,
	).Scan(&ok)
	return ok, err
}

func (s *Server) createUser(username, passwordHash, invite string) (string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Сериализует параллельные регистрации в пустой базе: иначе два первых
	// пользователя могли бы оба стать админами без приглашения.
	if _, err := tx.Exec(`LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return "", err
	}
	var anyUser bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM users)`).Scan(&anyUser); err != nil {
		return "", err
	}

	if anyUser {
		var code string
		err := tx.QueryRow(
			`SELECT code FROM invites
			  WHERE code = $1 AND used_by IS NULL AND expires_at > now()
			  FOR UPDATE`, invite,
		).Scan(&code)
		if errors.Is(err, sql.ErrNoRows) {
			return "", errInvite
		} else if err != nil {
			return "", err
		}
	}

	var id string
	err = tx.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3) RETURNING id`,
		username, passwordHash, !anyUser,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	if anyUser {
		if _, err := tx.Exec(
			`UPDATE invites SET used_by = $1, used_at = now() WHERE code = $2`, id, invite,
		); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	name := strings.TrimSpace(req.Username)
	if name == "" || len(name) > 4*maxUsernameLen {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	// Подбор пароля: после нескольких неудач — пауза между попытками (loginguard.go).
	if wait := s.logins.wait(name, time.Now()); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}

	var id, hash string
	err := s.DB.QueryRow(`SELECT id, password_hash FROM users WHERE username = $1`, name).Scan(&id, &hash)
	if err == sql.ErrNoRows {
		// Столько же времени, сколько проверка настоящего пароля: по времени
		// ответа не узнать, есть ли такой пользователь.
		auth.DummyVerify(req.Password)
		s.logins.failed(name, time.Now())
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	ok, err := auth.VerifyPassword(req.Password, hash)
	if errors.Is(err, auth.ErrBusy) {
		writeBusy(w)
		return
	}
	if err != nil || !ok {
		s.logins.failed(name, time.Now())
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.logins.succeeded(name)

	token, err := auth.IssueToken(id, tokenTTL, s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// handleMe tells the frontend who is logged in (and whether to show admin
// controls such as "Пригласить").
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	var username string
	var isAdmin bool
	err := s.DB.QueryRow(`SELECT username, is_admin FROM users WHERE id = $1`, userID).Scan(&username, &isAdmin)
	if err == sql.ErrNoRows {
		// Токен валиден, но пользователя уже нет — как просроченный токен.
		writeError(w, http.StatusUnauthorized, "user no longer exists")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// can_upload — показывать ли загрузку своих релизов (uploads.go);
	// lastfm_scrobble — слать ли плееру «сейчас играет» и скробблы (lastfm.go).
	l := s.lastfmLinkOf(userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": userID, "username": username, "is_admin": isAdmin, "can_upload": canUpload(r),
		"lastfm_scrobble": l != nil && l.scrobble,
	})
}

// handleCreateInvite (admin) issues a single-use registration code.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)

	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate invite")
		return
	}
	code := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	expires := time.Now().Add(inviteTTL)

	if _, err := s.DB.Exec(
		`INSERT INTO invites (code, created_by, expires_at) VALUES ($1, $2, $3)`, code, userID, expires,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create invite")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"code":       code,
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}
