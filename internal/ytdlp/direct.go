package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Media is format 140 of a video as yt-dlp extracted it: a googlevideo URL
// (valid only from this server's IP, for a few hours) and the headers to
// fetch it with.
type Media struct {
	URL     string
	Headers map[string]string
}

// Resolve runs only yt-dlp's extraction (`-j`, or helper.py after
// EnableWarm) for videoID, without its downloader. Measured on the server, `yt-dlp -o -` spends ~1 s between
// extraction and the first byte; fetching the URL ourselves (Fetch) gets
// it in ~0.1 s.
func Resolve(ctx context.Context, videoID string) (*Media, error) {
	if w := warm.Load(); w != nil {
		m, err := w.extract(ctx, videoID)
		if !errors.Is(err, errWarmUnavailable) {
			return m, err
		}
	}
	out, err := execCommand(ctx, "yt-dlp", "-j", "--no-warnings", "-f", "140",
		"https://www.youtube.com/watch?v="+videoID).Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp extract: %w", wrapWithStderr(err))
	}
	var info struct {
		URL      string            `json:"url"`
		Protocol string            `json:"protocol"`
		Headers  map[string]string `json:"http_headers"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("yt-dlp extract: %w", err)
	}
	return mediaFrom(info.URL, info.Protocol, info.Headers)
}

func mediaFrom(url, protocol string, headers map[string]string) (*Media, error) {
	if url == "" || protocol != "https" {
		// m3u8/dash-фрагменты — это не один файл; такое пусть качает сам yt-dlp.
		return nil, fmt.Errorf("%w (protocol %q)", errNotPlainFile, protocol)
	}
	return &Media{URL: url, Headers: headers}, nil
}

var errNotPlainFile = errors.New("yt-dlp extract: format 140 is not a plain https file")

// errFetch marks failures of the direct download (expired URL, 403, cut
// off), as opposed to the video itself being unavailable.
var errFetch = errors.New("fetching audio")

// httpClient fetches googlevideo URLs; the caller's context bounds it.
var httpClient = &http.Client{}

// fetchChunk: YouTube throttles a request for the whole file to a crawl
// (measured: ~30 KB/s), but serves Range requests of up to ~10 MB at full
// speed — yt-dlp downloads in such chunks too (http_chunk_size).
var fetchChunk int64 = 10 << 20

// Fetch downloads m with consecutive Range requests. The first one is made
// before returning, so an expired URL or 403 is an error here, not in the
// middle of the stream.
func Fetch(ctx context.Context, client *http.Client, m *Media) (io.ReadCloser, error) {
	f := &rangeFetcher{ctx: ctx, client: client, media: m, size: -1}
	if err := f.next(); err != nil {
		return nil, err
	}
	return f, nil
}

type rangeFetcher struct {
	ctx    context.Context
	client *http.Client
	media  *Media
	body   io.ReadCloser
	pos    int64 // сколько уже прочитано
	end    int64 // конец текущего куска (не включительно)
	size   int64 // полный размер из Content-Range; -1 — ещё неизвестен
}

func (f *rangeFetcher) next() error {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.media.URL, nil)
	if err != nil {
		return err
	}
	for k, v := range f.media.Headers {
		req.Header.Set(k, v)
	}
	last := f.pos + fetchChunk - 1
	if f.size >= 0 {
		last = min(last, f.size-1)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", f.pos, last))
	res, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errFetch, err)
	}
	if res.StatusCode != http.StatusPartialContent {
		res.Body.Close()
		return fmt.Errorf("%w: status %d", errFetch, res.StatusCode)
	}
	start, end, size, ok := parseContentRange(res.Header.Get("Content-Range"))
	if !ok || start != f.pos {
		res.Body.Close()
		return fmt.Errorf("%w: unexpected Content-Range %q", errFetch, res.Header.Get("Content-Range"))
	}
	f.body, f.end, f.size = res.Body, end+1, size
	return nil
}

func (f *rangeFetcher) Read(p []byte) (int, error) {
	for {
		if f.body == nil {
			if f.size >= 0 && f.pos >= f.size {
				return 0, io.EOF
			}
			if err := f.next(); err != nil {
				return 0, err
			}
		}
		n, err := f.body.Read(p)
		f.pos += int64(n)
		if err == io.EOF {
			f.body.Close()
			f.body = nil
			if f.pos != f.end {
				return n, fmt.Errorf("%w: chunk ended at %d, expected %d", errFetch, f.pos, f.end)
			}
			err = nil
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
}

func (f *rangeFetcher) Close() error {
	if f.body != nil {
		return f.body.Close()
	}
	return nil
}

// parseContentRange parses "bytes start-end/size".
func parseContentRange(h string) (start, end, size int64, ok bool) {
	spec, found := strings.CutPrefix(h, "bytes ")
	if !found {
		return 0, 0, 0, false
	}
	rng, total, found := strings.Cut(spec, "/")
	if !found {
		return 0, 0, 0, false
	}
	a, b, found := strings.Cut(rng, "-")
	if !found {
		return 0, 0, 0, false
	}
	var errs [3]error
	start, errs[0] = strconv.ParseInt(a, 10, 64)
	end, errs[1] = strconv.ParseInt(b, 10, 64)
	size, errs[2] = strconv.ParseInt(total, 10, 64)
	if errors.Join(errs[:]...) != nil || start > end || end >= size {
		return 0, 0, 0, false
	}
	return start, end, size, true
}
