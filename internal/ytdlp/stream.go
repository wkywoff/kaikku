package ytdlp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
)

// Stream starts `yt-dlp -f 140 -o -` for videoID and returns its stdout as
// soon as the first byte has arrived, so a failure that happens before any
// audio (unavailable video, 403, no format 140) is returned here rather than
// in the middle of the response. Measured on the server: the first byte
// comes ~1.6–1.9 s after start (extraction), the rest of a 4 MB track
// ~0.2 s later.
//
// Only format 140 (AAC in DASH MP4): it plays everywhere, including iOS
// Safari, and its sidx box gives the file size upfront (media.DashFileSize).
//
// Read returns yt-dlp's exit error (with its stderr) at the end of a failed
// download instead of io.EOF. Close kills the process if it's still running.
func Stream(ctx context.Context, videoID string) (io.ReadCloser, error) {
	cmd := execCommand(ctx, "yt-dlp",
		"--no-warnings", "--no-progress",
		"-f", "140",
		"-o", "-",
		"https://www.youtube.com/watch?v="+videoID,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{max: 2000}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("yt-dlp stream: %w", err)
	}
	s := &streamReader{cmd: cmd, r: bufio.NewReaderSize(stdout, 64<<10), stderr: stderr}
	if _, err := s.r.Peek(1); err != nil {
		if err := s.finish(); err != nil {
			return nil, err
		}
		return nil, errors.New("yt-dlp stream: no output")
	}
	return s, nil
}

type streamReader struct {
	cmd    *exec.Cmd
	r      *bufio.Reader
	stderr *tailBuffer
	wait   sync.Once
	err    error
}

func (s *streamReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err == io.EOF {
		if werr := s.finish(); werr != nil {
			return n, werr
		}
	}
	return n, err
}

func (s *streamReader) Close() error {
	_ = s.cmd.Process.Kill() // уже вышедшему процессу — безвредная ошибка
	_ = s.finish()
	return nil
}

// finish waits for the process once; the error carries yt-dlp's stderr.
func (s *streamReader) finish() error {
	s.wait.Do(func() {
		if err := s.cmd.Wait(); err != nil {
			s.err = fmt.Errorf("yt-dlp stream: %w: %s", err, strings.TrimSpace(s.stderr.String()))
		}
	})
	return s.err
}

// OpenStream is FindMatches + opening the audio: the best-ranked video
// that actually starts (up to maxTries candidates; each gets a second
// attempt, since YouTube periodically answers 403 on the first try — see
// Download).
func OpenStream(ctx context.Context, want Want, resultCount, toleranceMs, maxTries int) (videoID string, body io.ReadCloser, err error) {
	if want.VideoID != "" {
		for attempt := 0; attempt < 2; attempt++ {
			if body, err = openVideo(ctx, want.VideoID); err == nil {
				return want.VideoID, body, nil
			}
			log.Printf("stream %s: %v", want.VideoID, err)
			if ctx.Err() != nil {
				return "", nil, err
			}
		}
		if !want.SearchIfVideoFails {
			return "", nil, fmt.Errorf("video %s: %w", want.VideoID, err)
		}
	}
	ids, err := FindMatches(ctx, want, resultCount, toleranceMs)
	if err != nil {
		return "", nil, fmt.Errorf("match: %w", err)
	}
	for i, id := range ids {
		if i == maxTries {
			break
		}
		for attempt := 0; attempt < 2; attempt++ {
			body, err = openVideo(ctx, id)
			if err == nil {
				return id, body, nil
			}
			log.Printf("stream %s: %v", id, err)
			if ctx.Err() != nil {
				return "", nil, err
			}
		}
	}
	if err == nil {
		err = errors.New("no candidates")
	}
	return "", nil, err
}

// tailBuffer keeps the last max bytes written — enough of yt-dlp's stderr
// for an error message without growing on a long progress log.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// openVideo fetches the audio directly (Resolve + Fetch, the fast path);
// if that isn't possible for this video, yt-dlp downloads it itself (Stream).
func openVideo(ctx context.Context, videoID string) (io.ReadCloser, error) {
	m, err := Resolve(ctx, videoID)
	if err == nil {
		var body io.ReadCloser
		if body, err = Fetch(ctx, httpClient, m); err == nil {
			return body, nil
		}
	}
	if !errors.Is(err, errNotPlainFile) && !errors.Is(err, errFetch) {
		return nil, err // ролик недоступен — yt-dlp -o - упадёт так же
	}
	log.Printf("stream %s: %v, falling back to yt-dlp's downloader", videoID, err)
	return Stream(ctx, videoID)
}
