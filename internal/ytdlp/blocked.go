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

// markBlocked marks err as ErrBlocked when yt-dlp's output says YouTube
// refused the server.
func markBlocked(err error, output string) error {
	if err == nil || !isBlocked(output) {
		return err
	}
	return &blockedError{err}
}

// Proxied reports whether YouTube is reached through YTDLP_PROXY.
func Proxied() bool { return proxy != "" }
