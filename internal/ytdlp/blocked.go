package ytdlp

import (
	"errors"
	"strings"
)

// ErrBlocked: YouTube refuses this server rather than the video. It asks
// to sign in to confirm it's not a bot (what it tells many data-center
// IPs) or has rate-limited the session. Every other video fails the same
// way, so callers stop trying and don't count it against the track:
// errors.Is(err, ErrBlocked). A proxy (YTDLP_PROXY) usually fixes it.
var ErrBlocked = errors.New("youtube refuses this server")

// Так yt-dlp пишет об этом (2026-11): «Sign in to confirm you’re not a
// bot» и «The current session has been rate-limited by YouTube».
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

// ErrSignIn: YouTube wants a signed-in account for this video (18+, for
// channel members, private), and there are no cookies, or it doesn't take
// theirs: an account that hasn't confirmed its age gets "…please provide
// more info so we can be sure you're an adult". Other videos are fine:
// errors.Is(err, ErrSignIn).
var ErrSignIn = errors.New("youtube wants a sign-in for this video")

type signInError struct{ err error }

func (e *signInError) Error() string        { return e.err.Error() }
func (e *signInError) Unwrap() error        { return e.err }
func (e *signInError) Is(target error) bool { return target == ErrSignIn }

// AgeUnconfirmed reports whether YouTube refused an 18+ video to the
// cookies' account because it hasn't confirmed its age with Google ("…please
// provide more info so we can be sure you're an adult").
func AgeUnconfirmed(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "provide more info") || strings.Contains(s, "an adult")
}

// classify marks err by yt-dlp's output: ErrBlocked when YouTube refused
// the server, ErrSignIn when it wants a sign-in for the video.
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

// Так yt-dlp пишет, что YouTube хочет вход в аккаунт ради этого видео: 18+
// («Sign in to confirm your age», а с куками аккаунта, который не
// подтвердил возраст, — «…content is age-restricted…»), только для
// спонсоров канала, приватное. К таким ошибкам yt-dlp добавляет подсказку
// «Use --cookies-from-browser or --cookies».
var signInMarkers = []string{
	"--cookies", "confirm your age", "age-restricted", "inappropriate for some users",
	"members-only", "join this channel", "granted access",
}

// needsSignIn reports whether YouTube wants a signed-in account: for the
// whole server (isBlocked) or for this video.
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

// Proxied reports whether YouTube is reached through YTDLP_PROXY.
func Proxied() bool { return proxy != "" }
