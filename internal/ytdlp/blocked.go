package ytdlp

import (
	"errors"
	"strings"
)

var ErrBlocked = errors.New("youtube refuses this server")

var blockedMarkers = []string{"not a bot", "rate-limited by youtube"}

func isBlocked(output string) bool {
	s := strings.ToLower(output)
	for _, m := range blockedMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

type blockedError struct{ err error }

func (e *blockedError) Error() string        { return e.err.Error() }
func (e *blockedError) Unwrap() error        { return e.err }
func (e *blockedError) Is(target error) bool { return target == ErrBlocked }

var ErrSignIn = errors.New("youtube wants a sign-in for this video")

type signInError struct{ err error }

func (e *signInError) Error() string        { return e.err.Error() }
func (e *signInError) Unwrap() error        { return e.err }
func (e *signInError) Is(target error) bool { return target == ErrSignIn }

func AgeUnconfirmed(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "provide more info") || strings.Contains(s, "an adult")
}

func classify(err error, output string) error {
	switch {
	case err == nil:
		return nil
	case isBlocked(output):
		return &blockedError{err}
	case needsSignIn(output):
		return &signInError{err}
	}
	return err
}

var signInMarkers = []string{
	"--cookies", "confirm your age", "age-restricted", "inappropriate for some users",
	"members-only", "join this channel", "granted access",
}

func needsSignIn(output string) bool {
	if isBlocked(output) {
		return true
	}
	s := strings.ToLower(output)
	for _, m := range signInMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func Proxied() bool { return proxy != "" }
