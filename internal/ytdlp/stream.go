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

func Stream(ctx context.Context, videoID string) (io.ReadCloser, error) {
	signIn := cookiesFirst()
	for {
		s, err := startStream(ctx, videoID, signIn)
		if err == nil || signIn || ctx.Err() != nil || !signInNeeded(err.Error()) {
			return s, err
		}
		signIn = true
	}
}

func startStream(ctx context.Context, videoID string, signIn bool) (io.ReadCloser, error) {
	cmd, done := command(ctx, signIn,
		"--no-warnings", "--no-progress",
		"-f", "140",
		"-o", "-",
		"https://www.youtube.com/watch?v="+videoID,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		done()
		return nil, err
	}
	stderr := &tailBuffer{max: 2000}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		done()
		return nil, fmt.Errorf("yt-dlp stream: %w", err)
	}
	s := &streamReader{cmd: cmd, r: bufio.NewReaderSize(stdout, 64<<10), stderr: stderr, done: done}
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
	done   func()
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
	_ = s.cmd.Process.Kill()
	_ = s.finish()
	return nil
}

func (s *streamReader) finish() error {
	s.wait.Do(func() {
		err := s.cmd.Wait()
		s.done()
		if err != nil {
			stderr := strings.TrimSpace(s.stderr.String())
			s.err = classify(fmt.Errorf("yt-dlp stream: %w: %s", err, stderr), stderr)
		}
	})
	return s.err
}

func OpenStream(ctx context.Context, want Want, resultCount, toleranceMs, maxTries int) (videoID string, body io.ReadCloser, err error) {
	if want.VideoID != "" {
		for attempt := 0; attempt < 2; attempt++ {
			if body, err = openVideo(ctx, want.VideoID); err == nil {
				return want.VideoID, body, nil
			}
			log.Printf("stream %s: %v", want.VideoID, err)
			if ctx.Err() != nil || errors.Is(err, ErrBlocked) {
				return "", nil, err
			}
			if errors.Is(err, ErrSignIn) {
				break
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
			if ctx.Err() != nil || errors.Is(err, ErrBlocked) {
				return "", nil, err
			}
			if errors.Is(err, ErrSignIn) {
				break
			}
		}
	}
	if err == nil {
		err = errors.New("no candidates")
	}
	return "", nil, err
}

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

func openVideo(ctx context.Context, videoID string) (io.ReadCloser, error) {
	m, err := Resolve(ctx, videoID)
	if err == nil {
		var body io.ReadCloser
		if body, err = Fetch(ctx, httpClient, m); err == nil {
			return body, nil
		}
	}
	if !errors.Is(err, errNotPlainFile) && !errors.Is(err, errFetch) {
		return nil, err
	}
	log.Printf("stream %s: %v, falling back to yt-dlp's downloader", videoID, err)
	return Stream(ctx, videoID)
}
