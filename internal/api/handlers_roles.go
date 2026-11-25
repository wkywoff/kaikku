package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

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
		serverError(w, r, "update failed", err)
		return
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id::text FROM users WHERE is_admin ORDER BY id FOR UPDATE`)
	if err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	callerIsAdmin, others := false, 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			serverError(w, r, "update failed", err)
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
		serverError(w, r, "update failed", err)
		return
	}
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
		serverError(w, r, "update failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	log.Printf("users: admin rights of %s set to %v by user %s", username, *req.Admin, callerID)
	writeJSON(w, http.StatusOK, map[string]any{"id": userID, "username": username, "is_admin": *req.Admin})
}
