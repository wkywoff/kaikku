package lyrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	retryMissing = 7 * 24 * time.Hour
	retryBasic   = 30 * 24 * time.Hour
	findTimeout  = 8 * time.Second
)

type Service struct {
	DB        *sql.DB
	Providers []Provider

	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	res  *Lyrics
	err  error
}

func (s *Service) Get(ctx context.Context, t Track) (*Lyrics, error) {
	old, fresh, err := s.load(ctx, t)
	if err != nil {
		return nil, err
	} else if fresh {
		return old, nil
	}
	key := t.DeezerID
	if t.ISRC != "" {
		key = "isrc:" + t.ISRC
	}

	s.mu.Lock()
	if s.inflight == nil {
		s.inflight = map[string]*call{}
	}
	if c, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		select {
		case <-c.done:
			return c.res, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	s.inflight[key] = c
	s.mu.Unlock()

	var complete bool
	c.res, complete, c.err = s.find(context.WithoutCancel(ctx), t)
	if old != nil && c.err == nil && (c.res == nil || better(old, c.res)) {
		c.res, complete = old, true
	}
	if c.err == nil && complete {
		if err := s.save(context.WithoutCancel(ctx), t, c.res); err != nil {
			log.Printf("lyrics: save %s: %v", t.DeezerID, err)
		}
	}
	close(c.done)
	s.mu.Lock()
	delete(s.inflight, key)
	s.mu.Unlock()
	return c.res, c.err
}

func (s *Service) find(ctx context.Context, t Track) (best *Lyrics, complete bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	type result struct {
		name string
		l    *Lyrics
		err  error
	}
	results := make(chan result, len(s.Providers))
	pending := map[string]bool{}
	for _, p := range s.Providers {
		pending[p.Name()] = true
		go func(p Provider) {
			l, err := p.Find(ctx, t)
			results <- result{p.Name(), l, err}
		}(p)
	}
	var errs []error
	var failed []string
	for range s.Providers {
		r := <-results
		delete(pending, r.name)
		if r.err != nil {
			log.Printf("lyrics: %s %s: %v", r.name, t.DeezerID, r.err)
			errs = append(errs, r.err)
			failed = append(failed, r.name)
			continue
		}
		if r.l == nil || len(r.l.Lines) == 0 {
			continue
		}
		r.l.Source = r.name
		if best == nil || better(r.l, best) {
			best = r.l
		}
		if best.Sync.Rank() >= SyncWord.Rank() && !outranked(best, pending) {
			break
		}
	}
	if best == nil {
		if len(errs) > 0 {
			return nil, false, errors.Join(errs...)
		}
		return nil, true, nil
	}
	complete = true
	for _, name := range failed {
		if best.Sync == SyncNone || outranked(best, map[string]bool{name: true}) {
			complete = false
			log.Printf("lyrics: %s from %s, not saved: %s did not answer", t.DeezerID, best.Source, name)
		}
	}
	return best, complete, nil
}

func outranked(l *Lyrics, sources map[string]bool) bool {
	for name := range sources {
		if sourceOrder[name] > sourceOrder[l.Source] {
			return true
		}
	}
	return false
}

var sourceOrder = map[string]int{"lrcred": 3, "deezer": 2, "lrclib": 1}

const topSource = "lrcred"

func better(a, b *Lyrics) bool {
	if sa, sb := a.Sync != SyncNone, b.Sync != SyncNone; sa != sb {
		return sa
	}
	if wa, wb := a.Sync.Rank() >= SyncWord.Rank(), b.Sync.Rank() >= SyncWord.Rank(); wa != wb {
		return wa
	}
	if oa, ob := sourceOrder[a.Source], sourceOrder[b.Source]; oa != ob {
		return oa > ob
	}
	return a.Sync.Rank() > b.Sync.Rank()
}

func (s *Service) load(ctx context.Context, t Track) (*Lyrics, bool, error) {
	var body []byte
	var sync string
	var fetched time.Time
	err := s.DB.QueryRowContext(ctx, `
		SELECT body, sync, fetched_at FROM lyrics
		WHERE deezer_id = $1 OR ($2 <> '' AND isrc = $2)
		ORDER BY body IS NOT NULL DESC, fetched_at DESC
		LIMIT 1`, t.DeezerID, t.ISRC).Scan(&body, &sync, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	age := time.Since(fetched)
	if body == nil {
		return nil, age < retryMissing, nil
	}
	var l Lyrics
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, false, nil
	}
	return &l, Sync(sync).Rank() >= SyncWord.Rank() && l.Source == topSource || age < retryBasic, nil
}

func (s *Service) save(ctx context.Context, t Track, l *Lyrics) error {
	var body []byte
	source, sync := "", ""
	if l != nil {
		var err error
		if body, err = json.Marshal(l); err != nil {
			return err
		}
		source, sync = l.Source, string(l.Sync)
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO lyrics (deezer_id, isrc, source, sync, body, fetched_at) VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (deezer_id) DO UPDATE SET isrc = $2, source = $3, sync = $4, body = $5, fetched_at = now()`,
		t.DeezerID, t.ISRC, source, sync, body)
	return err
}

func (s *Service) Stored(ctx context.Context, deezerID string) (*Lyrics, error) {
	var body []byte
	err := s.DB.QueryRowContext(ctx, `SELECT body FROM lyrics WHERE deezer_id = $1`, deezerID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) || err == nil && body == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lyrics
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, nil
	}
	return &l, nil
}

func (s *Service) Put(ctx context.Context, deezerID string, l *Lyrics) error {
	if l == nil {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM lyrics WHERE deezer_id = $1`, deezerID)
		return err
	}
	return s.save(ctx, Track{DeezerID: deezerID}, l)
}

func (s *Service) Forget(ctx context.Context, deezerID, isrc string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM lyrics WHERE deezer_id = $1 OR ($2 <> '' AND isrc = $2)`, deezerID, isrc)
	return err
}
