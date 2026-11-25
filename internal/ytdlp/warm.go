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

//go:embed helper.py
var helperScript string

var warm atomic.Pointer[warmClient]

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

var errWarmUnavailable = errors.New("ytdlp helper unavailable")

const warmRestartDelay = time.Minute

const (
	warmSleepAfter   = 30 * time.Minute
	warmRecycleIdle  = 2 * time.Minute
	warmRecycleRSS   = 120 << 20
	warmRecycleAge   = 6 * time.Hour
	warmCheckEvery   = time.Minute
	warmMallocArenas = "MALLOC_ARENA_MAX=2"
)

type warmClient struct {
	mu       sync.Mutex
	proc     *warmProc
	failedAt time.Time
	lastUse  time.Time
}

type warmProc struct {
	stdin   io.WriteCloser
	dead    chan struct{}
	pid     int
	started time.Time
	cookies string

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan warmReply
	retired bool
}

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

func (c *warmClient) maintain(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.proc
	if p == nil {
		return
	}
	select {
	case <-p.dead:
		return
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
			np.retire()
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
			if c.proc.cookies == cookiesVersion() || !c.proc.retire() {
				return c.proc, nil
			}
			c.proc = nil
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
	version := cookiesVersion()
	cookies, dropCookies := cookieCopy()
	cmd := execCommand(context.Background(), "python3", "-c", helperScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		dropCookies()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		dropCookies()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if cmd.Env == nil {
		cmd.Env = toolEnv(os.Environ())
	}
	cmd.Env = append(cmd.Env, warmEnv(cookies)...)
	if err := cmd.Start(); err != nil {
		dropCookies()
		return nil, err
	}
	p := &warmProc{stdin: stdin, dead: make(chan struct{}), pid: cmd.Process.Pid, started: time.Now(),
		cookies: version, pending: map[int64]chan warmReply{}}
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
		dropCookies()
		log.Printf("ytdlp helper exited: %v", err)
		close(p.dead)
	}()
	return p, nil
}

func (c *warmClient) call(ctx context.Context, req map[string]any) (warmReply, error) {
	var p *warmProc
	for attempt := 0; ; attempt++ {
		var err error
		if p, err = c.process(); err != nil {
			return warmReply{}, err
		}
		p.mu.Lock()
		if !p.retired {
			break
		}
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
			return r, classify(errors.New(r.Error), r.Error)
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
