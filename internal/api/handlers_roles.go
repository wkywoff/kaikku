package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// Права админа (users.is_admin). Первым админом становится первый аккаунт
// (handlers_auth.go), дальше права выдаёт и снимает любой админ:
// PUT /api/admin/users/{id}/admin {"admin": true|false}. Действует сразу —
// authed и Subsonic читают is_admin на каждом запросе.
//
// Без админа сервис остаться не должен: вернуть права было бы некому, только
// руками в базе. Поэтому последнего админа снять нельзя — в том числе себя.
// С себя, пока есть другой админ, — можно: правило одно, «хоть один остаётся».

// handleSetUserAdmin (admin) -> { id, username, is_admin }.
func (s *Server) handleSetUserAdmin(w http.ResponseWriter, r *http.Request) {
	callerID := currentUser(r)
	userID := r.PathValue("id")
	var req struct {
		Admin *bool `json:"admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Admin == nil {
		writeError(w, http.StatusBadRequest, "admin is required")
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	defer tx.Rollback()
	// Строки всех админов — под замок до конца транзакции, по порядку id
	// (два запроса не заблокируют друг друга). Одновременные снятия идут по
	// очереди, и второй запрос видит уже то, что осталось после первого:
	// два админа не снимут друг друга разом.
	rows, err := tx.Query(`SELECT id::text FROM users WHERE is_admin ORDER BY id FOR UPDATE`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	callerIsAdmin, others := false, 0 // others — админы, кроме того, чьи права меняют
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		if id == callerID {
			callerIsAdmin = true
		}
		if id != userID {
			others++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	// adminOnly проверил права до транзакции; их могли снять, пока запрос шёл.
	if !callerIsAdmin {
		writeError(w, http.StatusForbidden, "admin only")
		return
	}
	if !*req.Admin && others == 0 {
		writeError(w, http.StatusConflict, "the last admin cannot be removed")
		return
	}
	var username string
	err = tx.QueryRow(`UPDATE users SET is_admin = $2 WHERE id::text = $1 RETURNING username`, userID, *req.Admin).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	log.Printf("users: admin rights of %s set to %v by user %s", username, *req.Admin, callerID)
	writeJSON(w, http.StatusOK, map[string]any{"id": userID, "username": username, "is_admin": *req.Admin})
}
