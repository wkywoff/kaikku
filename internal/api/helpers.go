package api

import (
	"crypto/rand"
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

// isUniqueViolation reports whether err is a Postgres unique_violation (23505).
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

// writeDeezerError maps a Deezer client error to a response: "no such
// object" is the caller's 404, everything else is an upstream failure (502).
func writeDeezerError(w http.ResponseWriter, prefix string, err error) {
	var apiErr *deezer.APIError
	if errors.As(err, &apiErr) && apiErr.NotFound() {
		writeError(w, http.StatusNotFound, prefix+": not found on deezer")
		return
	}
	writeError(w, http.StatusBadGateway, prefix+": "+err.Error())
}

var deezerIDRe = regexp.MustCompile(`^[0-9]{1,20}$`)

// validDeezerID guards client-supplied IDs before they become part of a
// Deezer URL path or a source_ref.
func validDeezerID(id string) bool { return deezerIDRe.MatchString(id) }

// Свой релиз, артист или трек kaikku (загрузка, uploads.go): 'k' + число из
// последовательности kaikku_ids (миграция 0022). В Deezer таких нет.
var localIDRe = regexp.MustCompile(`^k[1-9][0-9]{0,17}$`)

func isLocalID(id string) bool { return localIDRe.MatchString(id) }

// validCatalogID — ID из каталога: Deezer или свой.
func validCatalogID(id string) bool { return validDeezerID(id) || isLocalID(id) }

// errPanicked — работа в горутине запроса упала с паникой (она в логе).
var errPanicked = errors.New("internal error")

// guard выполняет f в горутине запроса: net/http ловит паники только в
// горутине самого запроса, паника в запущенной из него — роняет весь API.
func guard(name string, f func() error) (err error) {
	if safe.Run(name, func() { err = f() }) {
		return errPanicked
	}
	return err
}

// errLocalID: своему треку не нужен ни Deezer, ни YouTube — у него уже есть файл.
var errLocalID = errors.New("own uploads are not downloaded")

// removeStorageFiles deletes files (storage keys relative to StorageDir)
// after their rows are gone. Best-effort: a leftover file is only wasted
// space, never a broken catalog. Keys escaping StorageDir are refused.
func (s *Server) removeStorageFiles(keys ...string) {
	if s.StorageDir == "" {
		return
	}
	root := filepath.Clean(s.StorageDir)
	for _, key := range keys {
		keys = append(keys, sizedCopies(key)...) // уменьшенные копии обложки (covers.go)
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
