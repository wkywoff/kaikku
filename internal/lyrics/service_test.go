package lyrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping DB-backed test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM lyrics WHERE deezer_id LIKE 'test-lyrics-%'`)
		db.Close()
	})
	return db
}

type fakeProvider struct {
	name  string
	l     *Lyrics
	err   error
	calls atomic.Int32
	delay time.Duration
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Find(ctx context.Context, t Track) (*Lyrics, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)
	if f.l == nil {
		return nil, f.err
	}
	cp := *f.l
	return &cp, f.err
}

func lines(sync Sync) *Lyrics {
	return &Lyrics{Sync: sync, Lines: []Line{{Start: 1, End: 2, Text: "строка"}}}
}

func TestService_WordSyncWinsAtOnceOtherwiseBestWithPreferences(t *testing.T) {
	db := openTestDB(t)
	// Deezer по словам — сразу ответ, медленный источник не ждём.
	dz := &fakeProvider{name: "deezer", l: lines(SyncWord)}
	ne := &fakeProvider{name: "netease", l: lines(SyncLine), delay: 2 * time.Second}
	s := &Service{DB: db, Providers: []Provider{dz, ne}}
	started := time.Now()
	if l, err := s.Get(context.Background(), Track{DeezerID: "test-lyrics-1"}); err != nil || l == nil || l.Source != "deezer" || time.Since(started) > time.Second {
		t.Fatalf("got %+v, %v after %v", l, err, time.Since(started))
	}
	// Запомнено: второй раз источники не спрашиваются.
	if l, _ := s.Get(context.Background(), Track{DeezerID: "test-lyrics-1"}); l == nil || dz.calls.Load() != 1 {
		t.Fatalf("not cached: deezer calls %d", dz.calls.Load())
	}

	// По строкам у всех — по порядку: Deezer, LRCLIB, NetEase.
	s2 := &Service{DB: db, Providers: []Provider{
		&fakeProvider{name: "netease", l: lines(SyncLine)},
		&fakeProvider{name: "lrclib", l: lines(SyncLine)},
	}}
	if l, _ := s2.Get(context.Background(), Track{DeezerID: "test-lyrics-2"}); l == nil || l.Source != "lrclib" {
		t.Fatalf("line sync tie: %+v", l)
	}
	// Источники спрашиваются одновременно: три медленных — не втрое дольше.
	slow := func(name string) *fakeProvider {
		return &fakeProvider{name: name, l: lines(SyncLine), delay: 300 * time.Millisecond}
	}
	sp := &Service{DB: db, Providers: []Provider{slow("deezer"), slow("netease"), slow("lrclib")}}
	started = time.Now()
	if l, _ := sp.Get(context.Background(), Track{DeezerID: "test-lyrics-8"}); l == nil || l.Source != "deezer" || time.Since(started) > 700*time.Millisecond {
		t.Fatalf("parallel: %+v after %v", l, time.Since(started))
	}
	// Тайминг важнее источника: LRCLIB по строкам лучше текста Deezer.
	s3 := &Service{DB: db, Providers: []Provider{
		&fakeProvider{name: "deezer", l: lines(SyncNone)},
		&fakeProvider{name: "netease"},
		&fakeProvider{name: "lrclib", l: lines(SyncLine)},
	}}
	if l, _ := s3.Get(context.Background(), Track{DeezerID: "test-lyrics-3"}); l == nil || l.Source != "lrclib" {
		t.Fatalf("timing first: %+v", l)
	}
	// Только тексты — полный текст Deezer.
	s4 := &Service{DB: db, Providers: []Provider{
		&fakeProvider{name: "deezer", l: lines(SyncNone)},
		&fakeProvider{name: "netease", l: lines(SyncNone)},
	}}
	if l, _ := s4.Get(context.Background(), Track{DeezerID: "test-lyrics-6"}); l == nil || l.Source != "deezer" {
		t.Fatalf("text tie: %+v", l)
	}
}

func TestService_MissingIsRememberedUnlessAllFailed(t *testing.T) {
	db := openTestDB(t)
	none := &fakeProvider{name: "deezer"}
	s := &Service{DB: db, Providers: []Provider{none}}
	for i := 0; i < 2; i++ {
		if l, err := s.Get(context.Background(), Track{DeezerID: "test-lyrics-4"}); l != nil || err != nil {
			t.Fatalf("got %+v, %v", l, err)
		}
	}
	if none.calls.Load() != 1 {
		t.Errorf("missing lyrics not remembered: %d calls", none.calls.Load())
	}

	// Источник упал, а другие ничего не нашли — ошибка, ничего не запоминается.
	down := &fakeProvider{name: "netease", err: errors.New("timeout")}
	s2 := &Service{DB: db, Providers: []Provider{&fakeProvider{name: "deezer"}, down}}
	for i := 0; i < 2; i++ {
		if _, err := s2.Get(context.Background(), Track{DeezerID: "test-lyrics-7"}); err == nil {
			t.Fatal("want an error when a source is down and nothing was found")
		}
	}
	if down.calls.Load() != 2 {
		t.Fatalf("failure was cached: calls %d", down.calls.Load())
	}
}

// Источник выше по цепочке не ответил — найденное ниже отдаётся, но не
// запоминается: в следующий раз он может ответить, и текст будет лучше.
func TestService_BetterSourceDownIsAskedAgain(t *testing.T) {
	db := openTestDB(t)
	lrclib := &fakeProvider{name: "lrclib", err: errors.New("HTTP 503: busy")}
	netease := &fakeProvider{name: "netease", l: lines(SyncLine)}
	s := &Service{DB: db, Providers: []Provider{&fakeProvider{name: "deezer"}, lrclib, netease}}
	for i := 0; i < 2; i++ {
		if l, err := s.Get(context.Background(), Track{DeezerID: "test-lyrics-9"}); err != nil || l == nil || l.Source != "netease" {
			t.Fatalf("got %+v, %v", l, err)
		}
	}
	if lrclib.calls.Load() != 2 {
		t.Errorf("lrclib asked %d times, want 2 (result not saved)", lrclib.calls.Load())
	}
	// Ниже по цепочке не ответил — лучшее уже есть, запоминается.
	s2 := &Service{DB: db, Providers: []Provider{&fakeProvider{name: "lrclib", l: lines(SyncLine)}, &fakeProvider{name: "netease", err: errors.New("timeout")}}}
	s2.Get(context.Background(), Track{DeezerID: "test-lyrics-10"})
	var n int
	db.QueryRow(`SELECT count(*) FROM lyrics WHERE deezer_id = 'test-lyrics-10'`).Scan(&n)
	if n != 1 {
		t.Error("lrclib result must be saved when only netease failed")
	}
}

func TestService_ConcurrentRequestsShareOneSearch(t *testing.T) {
	db := openTestDB(t)
	slow := &fakeProvider{name: "apple", l: lines(SyncSyllable), delay: 100 * time.Millisecond}
	s := &Service{DB: db, Providers: []Provider{slow}}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, err := s.Get(context.Background(), Track{DeezerID: "test-lyrics-5"}); err != nil || l == nil {
				t.Errorf("got %+v, %v", l, err)
			}
		}()
	}
	wg.Wait()
	if slow.calls.Load() != 1 {
		t.Errorf("searched %d times", slow.calls.Load())
	}
}

func TestService_SameRecordingOnAnotherReleaseSharesLyrics(t *testing.T) {
	db := openTestDB(t)
	p := &fakeProvider{name: "lrclib", l: lines(SyncLine)}
	s := &Service{DB: db, Providers: []Provider{p}}
	single := Track{DeezerID: "test-lyrics-20", ISRC: "TESTLYRICS01", Title: "Song"}
	album := Track{DeezerID: "test-lyrics-21", ISRC: "TESTLYRICS01", Title: "Song"}
	if l, err := s.Get(context.Background(), single); err != nil || l == nil {
		t.Fatalf("single: %+v, %v", l, err)
	}
	l, err := s.Get(context.Background(), album)
	if err != nil || l == nil || l.Source != "lrclib" {
		t.Fatalf("album: %+v, %v", l, err)
	}
	if p.calls.Load() != 1 {
		t.Errorf("searched %d times for one recording", p.calls.Load())
	}
	// «Найти заново» на альбоме забывает и текст сингла.
	if err := s.Forget(context.Background(), album.DeezerID, album.ISRC); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM lyrics WHERE isrc = 'TESTLYRICS01'`).Scan(&n)
	if n != 0 {
		t.Errorf("%d rows left after Forget", n)
	}
}

func TestService_RecheckThatFindsNothingKeepsOldLyrics(t *testing.T) {
	db := openTestDB(t)
	old := lines(SyncLine)
	old.Source = "lrclib"
	body, _ := json.Marshal(old)
	if _, err := db.Exec(`INSERT INTO lyrics (deezer_id, source, sync, body, fetched_at)
		VALUES ('test-lyrics-30', 'lrclib', 'line', $1, now() - interval '40 days')`, body); err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{name: "lrclib"} // срок вышел, а теперь не находит
	s := &Service{DB: db, Providers: []Provider{p}}
	l, err := s.Get(context.Background(), Track{DeezerID: "test-lyrics-30"})
	if err != nil || l == nil || len(l.Lines) != 1 {
		t.Fatalf("got %+v, %v; want the old lyrics", l, err)
	}
	if p.calls.Load() != 1 {
		t.Errorf("expired lyrics must be searched again, calls=%d", p.calls.Load())
	}
	var fresh bool
	db.QueryRow(`SELECT fetched_at > now() - interval '1 minute' FROM lyrics WHERE deezer_id = 'test-lyrics-30'`).Scan(&fresh)
	if !fresh {
		t.Error("kept lyrics must be marked as checked now")
	}
}
