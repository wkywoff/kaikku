package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeYtDlp replaces yt-dlp with this test binary (TestHelperProcess), so
// the code under test runs real processes with pipes and exit codes but no
// network. Video IDs decide the behaviour:
//
//	ok*      — extraction gives a direct URL (served by a local server with
//	           Range support); `-o -` prints "audio:<id>";
//	gone*    — "ERROR: Video unavailable", exit 1, for everything;
//	flaky*   — fails like gone* on the first call, then like ok*;
//	partial* — `-o -` prints some audio, then fails;
//	expired* — the direct URL answers 403 (yt-dlp's own download works);
//	hls*     — format 140 is HLS, not a plain file;
//	noquick* — fails with the quick client (quickClientArgs), works without.
//
// Searches (--flat-playlist) print searchResults. `-o <template>` writes
// the audio to the template with ext m4a.
func fakeYtDlp(t *testing.T, searchResults string) *[]string {
	t.Helper()
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(id, "expired") || r.Header.Get("User-Agent") != "fake-ua" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader("audio:"+id))
	}))
	t.Cleanup(media.Close)

	var calls []string
	old := execCommand
	flakyState := t.TempDir() + "/flaky"
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, strings.Join(args, " "))
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--", name}, args...)...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "FAKE_SEARCH="+searchResults,
			"FAKE_FLAKY_STATE="+flakyState, "FAKE_MEDIA_URL="+media.URL)
		return cmd
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	sep := slices.Index(os.Args, "--")
	if os.Args[sep+1] == "python3" {
		fakeWarmHelper()
	}
	args := os.Args[sep+2:]
	if slices.Contains(args, "--flat-playlist") {
		fmt.Print(os.Getenv("FAKE_SEARCH"))
		os.Exit(0)
	}
	url := args[len(args)-1]
	id := url[strings.LastIndex(url, "=")+1:]
	fail := func() {
		fmt.Fprintf(os.Stderr, "ERROR: [youtube] %s: Video unavailable\n", id)
		os.Exit(1)
	}
	switch {
	case strings.HasPrefix(id, "gone"):
		fail()
	case strings.HasPrefix(id, "flaky"):
		state := os.Getenv("FAKE_FLAKY_STATE")
		if _, err := os.Stat(state); err != nil {
			os.WriteFile(state, nil, 0o644)
			fail()
		}
	case strings.HasPrefix(id, "noquick") && slices.Contains(args, quickClientArgs):
		fmt.Fprintf(os.Stderr, "ERROR: [youtube] %s: Requested format is not available\n", id)
		os.Exit(1)
	}
	if i := slices.Index(args, "-o"); i >= 0 && args[i+1] != "-" {
		if err := os.WriteFile(strings.ReplaceAll(args[i+1], "%(ext)s", "m4a"), []byte("audio:"+id), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if slices.Contains(args, "-j") { // извлечение без скачивания
		protocol := "https"
		if strings.HasPrefix(id, "hls") {
			protocol = "m3u8_native"
		}
		fmt.Printf(`{"id":%q,"format_id":"140","protocol":%q,"url":"%s/%s","http_headers":{"User-Agent":"fake-ua"}}`+"\n",
			id, protocol, os.Getenv("FAKE_MEDIA_URL"), id)
		os.Exit(0)
	}
	fmt.Print("audio:" + id)
	if strings.HasPrefix(id, "partial") {
		fail()
	}
	os.Exit(0)
}

func searchLine(id, title string, durationS int) string {
	return fmt.Sprintf(`{"id":%q,"title":%q,"channel":"Artist - Topic","duration":%d}`+"\n", id, title, durationS)
}

func TestStream_ReadsAudio(t *testing.T) {
	calls := fakeYtDlp(t, "")
	body, err := Stream(context.Background(), "okVideo")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != "audio:okVideo" {
		t.Fatalf("ReadAll = %q, %v", data, err)
	}
	if got := (*calls)[0]; !strings.Contains(got, "-f 140") || !strings.Contains(got, "-o -") {
		t.Errorf("yt-dlp args = %q, want format 140 to stdout", got)
	}
}

func TestStream_FailureBeforeAudioIsAnError(t *testing.T) {
	fakeYtDlp(t, "")
	_, err := Stream(context.Background(), "goneVideo")
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Fatalf("Stream(unavailable) error = %v, want yt-dlp's stderr in it", err)
	}
}

func TestStream_FailureMidwayIsAReadError(t *testing.T) {
	fakeYtDlp(t, "")
	body, err := Stream(context.Background(), "partialVideo")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err == nil || string(data) != "audio:partialVideo" {
		t.Fatalf("ReadAll = %q, %v; want the partial audio and an error", data, err)
	}
}

func TestOpenStream_FallsBackToNextCandidate(t *testing.T) {
	results := searchLine("goneBest", "Song", 200) + searchLine("flakySecond", "Song", 201) + searchLine("okThird", "Song", 202)
	calls := fakeYtDlp(t, results)
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000}

	id, body, err := OpenStream(context.Background(), want, 15, 4000, 3)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	// goneBest — две попытки, flakySecond — проходит со второй.
	if id != "flakySecond" || string(data) != "audio:flakySecond" {
		t.Fatalf("OpenStream = %q (%q), want flakySecond", id, data)
	}
	if len(*calls) != 5 { // поиск + 2 × goneBest + 2 × flakySecond (только извлечение)
		t.Errorf("yt-dlp calls = %d, want 5: %q", len(*calls), *calls)
	}
	if last := (*calls)[4]; !strings.Contains(last, "-j") || strings.Contains(last, "-o -") {
		t.Errorf("audio fetched by yt-dlp (%q), want directly from the extracted URL", last)
	}
}

// Заданное видео (ссылка админа, уже скачанное раньше) — без поиска; не
// открылось — поиск, только если это разрешено.
func TestOpenStream_GivenVideo(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("okFound", "Song", 200))
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000, VideoID: "okGiven"}
	id, body, err := OpenStream(context.Background(), want, 15, 4000, 3)
	if err != nil || id != "okGiven" {
		t.Fatalf("given video: %q %v", id, err)
	}
	body.Close()
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.Contains(c, "--flat-playlist") }) {
		t.Fatalf("searched although the video was given: %q", *calls)
	}

	want.VideoID = "goneGiven"
	if _, _, err := OpenStream(context.Background(), want, 15, 4000, 3); err == nil {
		t.Fatal("the admin's video is gone: an error, no search")
	}
	want.SearchIfVideoFails = true
	id, body, err = OpenStream(context.Background(), want, 15, 4000, 3)
	if err != nil || id != "okFound" {
		t.Fatalf("fallback to search: %q %v", id, err)
	}
	body.Close()
}

func TestOpenStream_FallsBackToYtDlpDownloader(t *testing.T) {
	for _, id := range []string{"expiredURL", "hlsOnly"} {
		results := searchLine(id, "Song", 200)
		calls := fakeYtDlp(t, results)
		got, body, err := OpenStream(context.Background(), Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 15, 4000, 3)
		if err != nil {
			t.Fatalf("%s: OpenStream: %v", id, err)
		}
		data, _ := io.ReadAll(body)
		body.Close()
		if got != id || string(data) != "audio:"+id {
			t.Errorf("%s: got %q (%q)", id, got, data)
		}
		if last := (*calls)[len(*calls)-1]; !strings.Contains(last, "-o -") {
			t.Errorf("%s: last yt-dlp call %q, want its own download", id, last)
		}
	}
}

func TestOpenStream_GivesUpAfterMaxTries(t *testing.T) {
	results := searchLine("goneA", "Song", 200) + searchLine("goneB", "Song", 200) + searchLine("okC", "Song", 200)
	fakeYtDlp(t, results)
	_, _, err := OpenStream(context.Background(), Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 15, 4000, 2)
	if err == nil {
		t.Fatal("OpenStream with 2 unavailable candidates and maxTries=2 succeeded")
	}
}

func TestFetch_ChunkedRangeRequests(t *testing.T) {
	content := strings.Repeat("0123456789", 5)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Range") == "" || r.Header.Get("X-Test") != "yes" {
			http.Error(w, "no range or headers", http.StatusBadRequest) // как YouTube: целиком — нельзя
			return
		}
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(content))
	}))
	defer srv.Close()
	old := fetchChunk
	fetchChunk = 7
	defer func() { fetchChunk = old }()

	body, err := Fetch(context.Background(), srv.Client(), &Media{URL: srv.URL, Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("%d requests before the first Read, want 1 (errors surface early)", n)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != content {
		t.Fatalf("ReadAll = %q, %v", data, err)
	}
	if n := requests.Load(); n != 8 { // ceil(50/7)
		t.Errorf("%d range requests, want 8", n)
	}
}

func TestFetch_ForbiddenIsAnErrorUpfront(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.Client(), &Media{URL: srv.URL})
	if !errors.Is(err, errFetch) {
		t.Fatalf("Fetch(403) error = %v, want errFetch", err)
	}
}

func TestParseContentRange(t *testing.T) {
	if s, e, n, ok := parseContentRange("bytes 0-99/4025466"); !ok || s != 0 || e != 99 || n != 4025466 {
		t.Errorf("valid header: %d %d %d %v", s, e, n, ok)
	}
	for _, h := range []string{"", "bytes */100", "bytes 5-1/100", "bytes 0-100/100", "items 0-1/2"} {
		if _, _, _, ok := parseContentRange(h); ok {
			t.Errorf("parseContentRange(%q) ok", h)
		}
	}
}

// fakeWarmHelper speaks helper.py's protocol with the same video-ID
// behaviours as the fake yt-dlp; crash* makes the helper exit.
func fakeWarmHelper() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID               int64
			Op, Query, Video string
		}
		json.Unmarshal(sc.Bytes(), &req)
		reply := map[string]any{"id": req.ID}
		switch {
		case req.Op == "search":
			var entries []json.RawMessage
			for _, line := range strings.Split(strings.TrimSpace(os.Getenv("FAKE_SEARCH")), "\n") {
				entries = append(entries, json.RawMessage(line))
			}
			reply["entries"] = entries
		case strings.HasPrefix(req.Video, "crash"):
			os.Exit(3)
		case strings.HasPrefix(req.Video, "gone"):
			reply["error"] = "ERROR: [youtube] " + req.Video + ": Video unavailable"
		default:
			reply["url"] = os.Getenv("FAKE_MEDIA_URL") + "/" + req.Video
			reply["protocol"] = "https"
			reply["http_headers"] = map[string]string{"User-Agent": "fake-ua"}
		}
		line, _ := json.Marshal(reply)
		fmt.Println(string(line))
	}
	os.Exit(0)
}

func enableWarmForTest(t *testing.T) {
	t.Helper()
	warm.Store(&warmClient{})
	t.Cleanup(func() { warm.Store(nil) })
}

func countCalls(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestWarm_SearchAndExtractionInOneProcess(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("okA", "Song", 200)+searchLine("okB", "Song", 200))
	enableWarmForTest(t)
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000}

	for i := 0; i < 2; i++ {
		id, body, err := OpenStream(context.Background(), want, 15, 4000, 3)
		if err != nil {
			t.Fatalf("OpenStream #%d: %v", i, err)
		}
		data, _ := io.ReadAll(body)
		body.Close()
		if id != "okA" || string(data) != "audio:okA" {
			t.Errorf("OpenStream #%d = %q (%q)", i, id, data)
		}
	}
	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "-c ") {
		t.Errorf("processes started: %q, want only the helper, once", *calls)
	}
}

func TestWarm_VideoErrorIsReturnedAsIs(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("goneA", "Song", 200))
	enableWarmForTest(t)
	_, _, err := OpenStream(context.Background(), Want{Artist: "Artist", Title: "Song", DurationMs: 200000}, 15, 4000, 3)
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Fatalf("OpenStream(unavailable) error = %v", err)
	}
	if len(*calls) != 1 {
		t.Errorf("processes started: %q, want only the helper (no yt-dlp fallback for a video error)", *calls)
	}
}

func TestWarm_HelperCrashFallsBackToYtDlp(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("crashA", "Song", 200))
	enableWarmForTest(t)
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000}

	id, body, err := OpenStream(context.Background(), want, 15, 4000, 3)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if id != "crashA" || string(data) != "audio:crashA" {
		t.Errorf("OpenStream = %q (%q)", id, data)
	}
	if countCalls(*calls, "-j --no-warnings -f 140") != 1 {
		t.Errorf("calls %q: want the extraction redone by yt-dlp after the helper died", *calls)
	}

	// Сразу после падения помощник не перезапускается — всё через yt-dlp.
	if _, err := FindMatches(context.Background(), want, 15, 4000); err != nil {
		t.Fatal(err)
	}
	if countCalls(*calls, "-c ") != 1 || countCalls(*calls, "-j --no-warnings --ignore-errors --flat-playlist") != 1 {
		t.Errorf("calls %q: want one helper start and the next search via yt-dlp", *calls)
	}
}

func openOK(t *testing.T, want Want) {
	t.Helper()
	id, body, err := OpenStream(context.Background(), want, 15, 4000, 3)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if id != "okA" || string(data) != "audio:okA" {
		t.Fatalf("OpenStream = %q (%q)", id, data)
	}
}

func waitDead(t *testing.T, p *warmProc) {
	t.Helper()
	select {
	case <-p.dead:
	case <-time.After(5 * time.Second):
		t.Fatal("old helper still running")
	}
}

func TestWarm_SleepsWhenIdleAndWakesWithoutFallback(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("okA", "Song", 200))
	enableWarmForTest(t)
	c := warm.Load()
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000}

	openOK(t, want)
	old := c.proc
	c.maintain(time.Now().Add(warmSleepAfter - time.Minute))
	if c.proc != old {
		t.Fatal("helper stopped before the idle time ran out")
	}
	c.maintain(time.Now().Add(warmSleepAfter + time.Minute))
	if c.proc != nil {
		t.Fatal("idle helper still running")
	}
	waitDead(t, old)

	openOK(t, want) // снова через помощника, а не через yt-dlp и без паузы после «падения»
	if countCalls(*calls, "-c ") != 2 || countCalls(*calls, "-j --no-warnings") != 0 {
		t.Errorf("calls %q: want the helper started again and no yt-dlp fallback", *calls)
	}
}

func TestWarm_RecyclesGrownHelperBetweenRequests(t *testing.T) {
	calls := fakeYtDlp(t, searchLine("okA", "Song", 200))
	enableWarmForTest(t)
	c := warm.Load()
	rss := int64(50 << 20)
	oldRSS := procRSS
	procRSS = func(int) int64 { return rss }
	t.Cleanup(func() { procRSS = oldRSS })
	want := Want{Artist: "Artist", Title: "Song", DurationMs: 200000}

	openOK(t, want)
	old := c.proc
	c.maintain(time.Now().Add(warmRecycleIdle + time.Second))
	if c.proc != old {
		t.Fatal("a small helper was recycled")
	}

	rss = warmRecycleRSS + 1
	c.maintain(time.Now().Add(time.Second)) // запрос только что был — не трогаем
	if c.proc != old {
		t.Fatal("recycled right after a request")
	}

	// Запрос в работе — не заменяем, даже в паузу.
	old.mu.Lock()
	old.pending[-1] = make(chan warmReply, 1)
	old.mu.Unlock()
	c.maintain(time.Now().Add(warmRecycleIdle + time.Second))
	if c.proc != old {
		t.Fatal("recycled a helper with a request in flight")
	}
	old.mu.Lock()
	delete(old.pending, -1)
	old.mu.Unlock()

	c.maintain(time.Now().Add(warmRecycleIdle + time.Second))
	if c.proc == old || c.proc == nil {
		t.Fatal("grown helper not replaced")
	}
	waitDead(t, old)

	openOK(t, want)
	if countCalls(*calls, "-c ") != 2 || countCalls(*calls, "-j --no-warnings") != 0 {
		t.Errorf("calls %q: want 2 helper starts and no yt-dlp fallback", *calls)
	}
}

func downloadOK(t *testing.T, videoID string) string {
	t.Helper()
	dir := t.TempDir()
	path, err := Download(context.Background(), videoID, dir, "track")
	if err != nil {
		t.Fatalf("Download(%s): %v", videoID, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "audio:"+videoID {
		t.Fatalf("downloaded %q = %q, %v", path, data, err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("staging has %d files, want only the track", len(left))
	}
	return path
}

func TestDownload_UsesQuickClient(t *testing.T) {
	calls := fakeYtDlp(t, "")
	downloadOK(t, "okVideo")
	if len(*calls) != 1 || !strings.Contains((*calls)[0], quickClientArgs+" -f 140 ") {
		t.Errorf("calls %q: want one yt-dlp run with the quick client and format 140", *calls)
	}
}

func TestDownload_FallsBackToDefaultClients(t *testing.T) {
	calls := fakeYtDlp(t, "")
	downloadOK(t, "noquickVideo")
	if len(*calls) != 2 || strings.Contains((*calls)[1], "--extractor-args") || !strings.Contains((*calls)[1], "-f 140/bestaudio") {
		t.Errorf("calls %q: want the quick client, then the default clients with 140/bestaudio", *calls)
	}
}

func TestDownload_UnavailableVideoFailsAfterAllAttempts(t *testing.T) {
	calls := fakeYtDlp(t, "")
	dir := t.TempDir()
	_, err := Download(context.Background(), "goneVideo", dir, "track")
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Fatalf("Download(unavailable) error = %v, want yt-dlp's output in it", err)
	}
	if len(*calls) != 3 {
		t.Errorf("calls %q: want the quick client and two default attempts", *calls)
	}
}
