package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kaikku/internal/auth"
)

// Смена и сброс пароля (миграция 0016).
//
//   - Сам: PUT /api/me/password {old_password, new_password} — со старым
//     паролем; неудачные попытки сдерживает та же пауза, что и вход
//     (loginguard.go).
//   - Забыл: админ создаёт одноразовую ссылку /?reset=<код> на 24 часа
//     (почты у пользователей нет), человек по ней сам задаёт новый пароль —
//     админ его не видит. В базе — только sha256 кода.
//
// После смены или сброса токены, выданные раньше, не действуют (authed):
// остальные устройства выходят, а ответ несёт новый токен для этого.

const passwordResetTTL = 24 * time.Hour

func validNewPassword(p string) bool {
	return len(p) >= minPasswordLen && len(p) <= auth.MaxPasswordLen
}

func resetCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(code))))
	return hex.EncodeToString(sum[:])
}

// handleAdminUsers (admin) — кому можно сбросить пароль.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`SELECT id, username, is_admin FROM users ORDER BY lower(username)`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	type user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}
	users := []user{}
	for rows.Next() {
		var u user
		if rows.Scan(&u.ID, &u.Username, &u.IsAdmin) == nil {
			users = append(users, u)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleCreatePasswordReset (admin) issues a one-time link for the user;
// an older unused link of theirs stops working.
func (s *Server) handleCreatePasswordReset(w http.ResponseWriter, r *http.Request) {
	adminID := currentUser(r)
	userID := r.PathValue("id")
	var username string
	if err := s.DB.QueryRow(`SELECT username FROM users WHERE id::text = $1`, userID).Scan(&username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, "query failed")
		}
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate a link")
		return
	}
	code := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	expires := time.Now().Add(passwordResetTTL)
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create a link")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create a link")
		return
	}
	if _, err := tx.Exec(`INSERT INTO password_resets (code_hash, user_id, created_by, expires_at) VALUES ($1, $2, $3, $4)`,
		resetCodeHash(code), userID, adminID, expires); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create a link")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create a link")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"code":       code,
		"username":   username,
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}

// handlePasswordReset ({code, password}, no auth) sets the new password by
// a reset link and logs the person in.
func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNewPassword(req.Password) {
		writeError(w, http.StatusBadRequest, "password: 8–256 characters")
		return
	}
	// Ссылку — до хеширования: по чужой или старой ссылке CPU не тратится.
	hash := resetCodeHash(req.Code)
	var userID, username string
	err := s.DB.QueryRow(
		`SELECT r.user_id, u.username FROM password_resets r JOIN users u ON u.id = r.user_id
		  WHERE r.code_hash = $1 AND r.used_at IS NULL AND r.expires_at > now()`, hash,
	).Scan(&userID, &username)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "the link is invalid, used or expired")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	passwordHash, err := auth.HashPassword(req.Password)
	if errors.Is(err, auth.ErrBusy) {
		writeBusy(w)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return
	}
	defer tx.Rollback()
	// Пока хешировали, ссылку могли использовать — второй раз она не сработает.
	res, err := tx.Exec(`UPDATE password_resets SET used_at = now() WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()`, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writeError(w, http.StatusForbidden, "the link is invalid, used or expired")
		return
	}
	if !s.setPassword(w, tx, userID, passwordHash) {
		return
	}
	s.logins.succeeded(username) // пауза после неудачных входов — снята
	s.writeFreshToken(w, userID, username)
}

// handleChangePassword (PUT /api/me/password) — с текущим паролем.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	var req struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validNewPassword(req.New) || len(req.Old) > auth.MaxPasswordLen {
		writeError(w, http.StatusBadRequest, "new password: 8–256 characters")
		return
	}
	var username, hash string
	if err := s.DB.QueryRow(`SELECT username, password_hash FROM users WHERE id = $1`, userID).Scan(&username, &hash); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// Подбор старого пароля с чужого открытого устройства — как подбор при входе.
	if wait := s.logins.wait(username, time.Now()); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	ok, err := auth.VerifyPassword(req.Old, hash)
	if errors.Is(err, auth.ErrBusy) {
		writeBusy(w)
		return
	}
	if err != nil || !ok {
		s.logins.failed(username, time.Now())
		writeError(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	s.logins.succeeded(username)
	newHash, err := auth.HashPassword(req.New)
	if errors.Is(err, auth.ErrBusy) {
		writeBusy(w)
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return
	}
	defer tx.Rollback()
	if !s.setPassword(w, tx, userID, newHash) {
		return
	}
	s.writeFreshToken(w, userID, username)
}

// setPassword stores the hash, ends every older session (password_changed_at)
// and drops the user's unused reset links; commits tx.
func (s *Server) setPassword(w http.ResponseWriter, tx *sql.Tx, userID, hash string) bool {
	if _, err := tx.Exec(`UPDATE users SET password_hash = $2, password_changed_at = now() WHERE id = $1`, userID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return false
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return false
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return false
	}
	return true
}

// writeFreshToken answers with a token issued after the change. Tokens carry
// whole seconds and authed rejects those from the second of the change, so
// the new one waits for the next second (at most 1 s).
func (s *Server) writeFreshToken(w http.ResponseWriter, userID, username string) {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	token, err := auth.IssueToken(userID, tokenTTL, s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "username": username})
}
