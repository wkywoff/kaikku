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
	pbkdf2Iterations = 210_000
	saltLen          = 16
	keyLen           = 32

	MaxPasswordLen = 256
)

var (
	ErrPasswordTooLong = fmt.Errorf("password longer than %d bytes", MaxPasswordLen)
	ErrBusy            = errors.New("too many password checks at once")
)

const (
	hashParallel = 2
	hashWait     = 10 * time.Second
)

var hashSlots = make(chan struct{}, hashParallel)

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

const dummyHash = "pbkdf2-sha256$210000$bXVzaWNzdHJlYW0tZHVtbQ$632HfEaVL/+d7gQ5hHS147AP065sB+8PAoRwR6dfTeY"

func DummyVerify(password string) {
	_, _ = VerifyPassword(password, dummyHash)
}
