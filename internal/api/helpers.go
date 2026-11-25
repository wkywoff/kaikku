package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
	"kaikku/internal/safe"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	logServerError(r, msg, err)
	writeError(w, http.StatusInternalServerError, msg)
}

func logServerError(r *http.Request, msg string, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	log.Printf("%s %s: %s: %v", r.Method, r.URL.EscapedPath(), msg, err)
}

func closeRows(what string, rows *sql.Rows) {
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("%s: reading rows: %v", what, err)
	}
}

func isUniqueViolation(err error) bool {
	pqErr, ok := err.(*pq.Error)
	return ok && pqErr.Code == "23505"
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func writeDeezerError(w http.ResponseWriter, prefix string, err error) {
	var apiErr *deezer.APIError
	if errors.As(err, &apiErr) && apiErr.NotFound() {
		writeError(w, http.StatusNotFound, prefix+": not found on deezer")
		return
	}
	writeError(w, http.StatusBadGateway, prefix+": "+err.Error())
}

var deezerIDRe = regexp.MustCompile(`^[0-9]{1,20}$`)

func validDeezerID(id string) bool { return deezerIDRe.MatchString(id) }

var localIDRe = regexp.MustCompile(`^k[1-9][0-9]{0,17}$`)

func isLocalID(id string) bool { return localIDRe.MatchString(id) }

func validCatalogID(id string) bool { return validDeezerID(id) || isLocalID(id) }

var errPanicked = errors.New("internal error")

func guard(name string, f func() error) (err error) {
	if safe.Run(name, func() { err = f() }) {
		return errPanicked
	}
	return err
}

var errLocalID = errors.New("own uploads are not downloaded")

func (s *Server) removeStorageFiles(keys ...string) {
	if s.StorageDir == "" {
		return
	}
	root := filepath.Clean(s.StorageDir)
	for _, key := range keys {
		keys = append(keys, sizedCopies(key)...)
	}
	for _, key := range keys {
		path := filepath.Join(root, key)
		if !strings.HasPrefix(path, root+string(filepath.Separator)) {
			log.Printf("refusing to delete %q: outside storage dir", key)
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("removing %s: %v", path, err)
		}
	}
}

func randomKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
