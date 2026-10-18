package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	pbkdf2Iterations = 210_000 // OWASP 2023 minimum recommendation for PBKDF2-HMAC-SHA256
	saltLen          = 16
	keyLen           = 32

	// MaxPasswordLen bounds a password, so a request can't make hashing
	// arbitrarily expensive (the API also caps JSON bodies, router.go).
	MaxPasswordLen = 256
)

var (
	ErrPasswordTooLong = fmt.Errorf("password longer than %d bytes", MaxPasswordLen)
	// ErrBusy: too many password checks are already running.
	ErrBusy = errors.New("too many password checks at once")
)

// Хеш пароля — ~0.2 с процессора, а на сервере один vCPU: одновременно не
// больше hashParallel проверок, остальные ждут не дольше hashWait. Иначе
// поток попыток входа отнимал бы весь процессор у музыки.
const (
	hashParallel = 2
	hashWait     = 10 * time.Second
)

var hashSlots = make(chan struct{}, hashParallel)

// derive is PBKDF2-HMAC-SHA256 (RFC 8018) from the standard library: the
// HMAC is keyed once per call. The hand-written version this replaces
// re-keyed it on every one of the 210 000 iterations, so a long password
// cost minutes of CPU. The output is the same, so existing hashes verify.
func derive(password string, salt []byte, iterations, n int) ([]byte, error) {
	timer := time.NewTimer(hashWait)
	defer timer.Stop()
	select {
	case hashSlots <- struct{}{}:
	case <-timer.C:
		return nil, ErrBusy
	}
	defer func() { <-hashSlots }()
	return pbkdf2.Key(sha256.New, password, salt, iterations, n)
}

// HashPassword returns a self-describing string:
// pbkdf2-sha256$<iterations>$<salt_b64>$<hash_b64>
//
// PBKDF2 at this iteration count is an accepted choice (Django's default)
// and needs nothing beyond the standard library.
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordLen {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	digest, err := derive(password, salt, pbkdf2Iterations, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	), nil
}

// VerifyPassword checks password against a hash produced by HashPassword.
// A password over MaxPasswordLen can't have been stored, so it's rejected
// without hashing.
func VerifyPassword(password, encoded string) (bool, error) {
	if len(password) > MaxPasswordLen {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, fmt.Errorf("unrecognized password hash format")
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, fmt.Errorf("bad iteration count: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false, fmt.Errorf("bad salt encoding: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, fmt.Errorf("bad hash encoding: %w", err)
	}
	got, err := derive(password, salt, iterations, len(want))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash — хеш постороннего пароля (с теми же параметрами, что у
// настоящих), чтобы DummyVerify тратил столько же времени.
const dummyHash = "pbkdf2-sha256$210000$bXVzaWNzdHJlYW0tZHVtbQ$632HfEaVL/+d7gQ5hHS147AP065sB+8PAoRwR6dfTeY"

// DummyVerify takes as long as VerifyPassword does for a real user. The
// login handler calls it for unknown usernames, so the response time
// doesn't tell which usernames exist.
func DummyVerify(password string) {
	_, _ = VerifyPassword(password, dummyHash)
}
