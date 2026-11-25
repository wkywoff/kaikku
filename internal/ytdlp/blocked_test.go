package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestBlocked_MarksOnlyServerWideRefusals(t *testing.T) {
	base := errors.New("exit status 1")
	for out, want := range map[string]bool{
		"ERROR: [youtube] x: Sign in to confirm you’re not a bot. Use --cookies":                true,
		"ERROR: [youtube] x: Sign in to confirm you're not a bot":                               true,
		"ERROR: [youtube] x: The current session has been rate-limited by YouTube for up to an": true,
		"ERROR: [youtube] x: Video unavailable":                                                 false,
		"ERROR: [youtube] x: Sign in to confirm your age":                                       false,
	} {
		err := classify(base, out)
		if errors.Is(err, ErrBlocked) != want {
			t.Errorf("%q: blocked = %v, want %v", out, !want, want)
		}
		if !errors.Is(err, base) || err.Error() != base.Error() {
			t.Errorf("%q: the original error is lost: %v", out, err)
		}
	}
	if classify(nil, "not a bot") != nil {
		t.Error("no error became an error")
	}
	if !errors.Is(fmt.Errorf("download: %w", classify(base, "not a bot")), ErrBlocked) {
		t.Error("wrapping hides ErrBlocked")
	}
}

func TestDownload_BlockedStopsAfterDefaultClients(t *testing.T) {
	calls := fakeYtDlp(t, "")
	_, err := Download(context.Background(), "blockedVideo", t.TempDir(), "x")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("Download: %v, want ErrBlocked", err)
	}
	if len(*calls) != 2 {
		t.Errorf("yt-dlp ran %d times, want 2: %q", len(*calls), *calls)
	}
}

func TestFindMatches_BlockedSearchStopsTheCascade(t *testing.T) {
	calls := fakeYtDlp(t, "BLOCKED")
	_, err := FindMatches(context.Background(), Want{Artist: "Artist", Title: "Song", Album: "Album", DurationMs: 200000}, 15, 4000)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("FindMatches: %v, want ErrBlocked", err)
	}
	if len(*calls) != 1 {
		t.Errorf("searches: %q, want only the first one", *calls)
	}
}

func TestOpenStream_BlockedGivesUpAtOnce(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("blockedA", "Song", 200)+searchLine("okB", "Song", 200))
	_, _, err := OpenStream(context.Background(), Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 15, 4000, 3)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("OpenStream: %v, want ErrBlocked", err)
	}
	if len(*calls) != 2 {
		t.Errorf("yt-dlp calls: %q", *calls)
	}
}

func TestBlocked_FromStreamAndHelper(t *testing.T) {
	calls := fakeYtDlp(t, "")
	if _, err := Stream(context.Background(), "blockedV"); !errors.Is(err, ErrBlocked) {
		t.Errorf("Stream: %v, want ErrBlocked", err)
	}
	enableWarmForTest(t)
	if _, err := Resolve(context.Background(), "blockedV"); !errors.Is(err, ErrBlocked) {
		t.Errorf("Resolve via the helper: %v, want ErrBlocked", err)
	}
	if n := countCalls(*calls, "-c "); n != 1 {
		t.Errorf("helper started %d times: %q", n, *calls)
	}
}
