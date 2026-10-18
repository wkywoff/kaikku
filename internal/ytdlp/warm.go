package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// helper.py runs yt-dlp as a library in one long-running Python process,
// so a search or an extraction doesn't pay for starting Python and loading
// yt-dlp each time (~0.5 s per call on the 1-vCPU server). The API uses it
// for playing tracks that aren't in the library yet, where every second
// before the first sound counts; the worker keeps starting yt-dlp per call.
//
//go:embed helper.py
var helperScript string

var warm atomic.Pointer[warmClient]

// EnableWarm makes search and Resolve go through helper.py (started right
// away, restarted if it dies). Whenever the helper can't be used, they fall
// back to running yt-dlp as before. The helper sleeps when nobody uses it
// and is replaced when it has grown (maintain).
func EnableWarm() {
	c := &warmClient{}
	warm.Store(c)
	go func() {
		if _, err := c.process(); err != nil {
			log.Printf("ytdlp helper: %v", err)
		}
	}()
	go func() {
		for now := range time.Tick(warmCheckEvery) {
			c.maintain(now)
		}
	}()
}

// errWarmUnavailable: the helper itself failed (not the request) — the
// caller should run yt-dlp directly instead.
var errWarmUnavailable = errors.New("ytdlp helper unavailable")

// Если помощник упал, следующий запуск — не раньше чем через это время, а
// пока запросы идут через обычный yt-dlp (иначе каждый клик ждал бы
// заведомо неудачный старт).
const warmRestartDelay = time.Minute

// Память помощника. Python не отдаёт её обратно, а yt-dlp копит кеши (JS
// плеера YouTube — в каждом из экземпляров YoutubeDL на поток): за полтора
// часа 50 → 136 МБ на сервере с 2 ГБ. Поэтому:
//   - полчаса без запросов — помощник выключается (warmSleepAfter), следующий
//     запрос запускает его заново (~1 с, как без помощника);
//   - вырос или работает давно — в первую же паузу заменяется свежим
//     (warmRecycle*), без ожидания для слушателя.
const (
	warmSleepAfter   = 30 * time.Minute
	warmRecycleIdle  = 2 * time.Minute
	warmRecycleRSS   = 120 << 20
	warmRecycleAge   = 6 * time.Hour
	warmCheckEvery   = time.Minute
	warmMallocArenas = "MALLOC_ARENA_MAX=2" // меньше фрагментации glibc при 4 потоках помощника
)

type warmClient struct {
	mu       sync.Mutex
	proc     *warmProc
	failedAt time.Time
	lastUse  time.Time
}

type warmProc struct {
	stdin   io.WriteCloser
	dead    chan struct{} // закрыт, когда процесс завершился
	pid     int
	started time.Time

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan warmReply
	retired bool // выключен (stdin закрыт): новые запросы — в другой процесс
}

// procRSS — сколько памяти занимает процесс (VmRSS), 0 — не узнать.
// Подменяется в тестах.
var procRSS = func(pid int) int64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("VmRSS:")); ok {
			kb, _ := strconv.ParseInt(string(bytes.TrimSuffix(bytes.TrimSpace(rest), []byte(" kB"))), 10, 64)
			return kb << 10
		}
	}
	return 0
}

// retire выключает помощника, если у него нет запросов в работе: закрытый
// stdin — конец ввода, Python доделывает начатое и выходит. false — занят.
func (p *warmProc) retire() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.retired {
		return true
	}
	if len(p.pending) > 0 {
		return false
	}
	p.retired = true
	p.stdin.Close()
	return true
}

func (p *warmProc) busy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending) > 0
}

// maintain усыпляет простаивающего помощника и заменяет разросшегося;
// EnableWarm вызывает его раз в минуту.
func (c *warmClient) maintain(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.proc
	if p == nil {
		return
	}
	select {
	case <-p.dead:
		return // упал — разберётся process()
	default:
	}
	idle := now.Sub(c.lastUse)
	switch {
	case idle >= warmSleepAfter:
		if p.retire() {
			c.proc = nil
			log.Printf("ytdlp helper: asleep after %v idle", idle.Round(time.Minute))
		}
	case idle >= warmRecycleIdle:
		rss, age := procRSS(p.pid), now.Sub(p.started)
		if rss <= warmRecycleRSS && age <= warmRecycleAge || p.busy() {
			return
		}
		np, err := startWarm()
		if err != nil {
			log.Printf("ytdlp helper: recycle: %v", err)
			return
		}
		if !p.retire() {
			np.retire() // пока запускали, пришёл запрос — заменим в следующую паузу
			return
		}
		c.proc = np
		log.Printf("ytdlp helper: recycled (RSS %d MB, age %v)", rss>>20, age.Round(time.Minute))
	}
}

type warmReply struct {
	ID      int64             `json:"id"`
	Error   string            `json:"error"`
	Entries []ytDlpEntry      `json:"entries"`
	URL     string            `json:"url"`
	Proto   string            `json:"protocol"`
	Headers map[string]string `json:"http_headers"`
}

func (c *warmClient) process() (*warmProc, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUse = time.Now()
	if c.proc != nil {
		select {
		case <-c.proc.dead:
			c.proc = nil
			c.failedAt = time.Now()
		default:
			return c.proc, nil
		}
	}
	if time.Since(c.failedAt) < warmRestartDelay {
		return nil, errWarmUnavailable
	}
	p, err := startWarm()
	if err != nil {
		c.failedAt = time.Now()
		return nil, fmt.Errorf("%w: %v", errWarmUnavailable, err)
	}
	c.proc = p
	return p, nil
}

func startWarm() (*warmProc, error) {
	cmd := execCommand(context.Background(), "python3", "-c", helperScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr // трейсбек, если помощник упадёт
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, warmMallocArenas)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &warmProc{stdin: stdin, dead: make(chan struct{}), pid: cmd.Process.Pid, started: time.Now(),
		pending: map[int64]chan warmReply{}}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			var r warmReply
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				log.Printf("ytdlp helper: bad reply: %v", err)
				continue
			}
			p.mu.Lock()
			ch := p.pending[r.ID]
			delete(p.pending, r.ID)
			p.mu.Unlock()
			if ch != nil {
				ch <- r
			}
		}
		err := cmd.Wait()
		log.Printf("ytdlp helper exited: %v", err)
		close(p.dead)
	}()
	return p, nil
}

// call sends one request and waits for its reply. errWarmUnavailable means
// the helper failed (the caller falls back to the yt-dlp binary); a reply's
// own error is yt-dlp's answer to this request.
func (c *warmClient) call(ctx context.Context, req map[string]any) (warmReply, error) {
	var p *warmProc
	for attempt := 0; ; attempt++ {
		var err error
		if p, err = c.process(); err != nil {
			return warmReply{}, err
		}
		p.mu.Lock()
		if !p.retired {
			break // p.mu остаётся взятым — запрос регистрируется ниже
		}
		// maintain выключил этот процесс, пока мы его брали: c.proc уже другой.
		p.mu.Unlock()
		if attempt > 0 {
			return warmReply{}, fmt.Errorf("%w: helper retired", errWarmUnavailable)
		}
	}
	ch := make(chan warmReply, 1)
	p.nextID++
	id := p.nextID
	p.pending[id] = ch
	req["id"] = id
	line, _ := json.Marshal(req)
	_, err := p.stdin.Write(append(line, '\n'))
	p.mu.Unlock()
	if err != nil {
		return warmReply{}, fmt.Errorf("%w: %v", errWarmUnavailable, err)
	}

	select {
	case r := <-ch:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-p.dead:
		return warmReply{}, fmt.Errorf("%w: helper exited", errWarmUnavailable)
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return warmReply{}, ctx.Err()
	}
}

func (c *warmClient) search(ctx context.Context, query string) ([]candidate, error) {
	r, err := c.call(ctx, map[string]any{"op": "search", "query": query})
	if err != nil {
		return nil, err
	}
	results := make([]candidate, 0, len(r.Entries))
	for _, e := range r.Entries {
		results = append(results, e.candidate())
	}
	return results, nil
}

func (c *warmClient) extract(ctx context.Context, videoID string) (*Media, error) {
	r, err := c.call(ctx, map[string]any{"op": "extract", "video": videoID})
	if err != nil {
		return nil, err
	}
	return mediaFrom(r.URL, r.Proto, r.Headers)
}
