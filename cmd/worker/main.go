package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"kaikku/internal/db"
	"kaikku/internal/deezer"
	"kaikku/internal/ingest"
	"kaikku/internal/safe"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kaikku:kaikku@localhost:5432/kaikku?sslmode=disable"
	}
	storageDir := os.Getenv("STORAGE_DIR")
	if storageDir == "" {
		storageDir = "./data/storage"
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		log.Fatalf("storage dir: %v", err)
	}
	// STAGING_DIR — временное место для сырого аудио от yt-dlp до транскода.
	stagingDir := os.Getenv("STAGING_DIR")
	if stagingDir == "" {
		stagingDir = "./data/staging"
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		log.Fatalf("staging dir: %v", err)
	}

	conn, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	if err := ingest.CleanStaging(stagingDir); err != nil {
		log.Fatalf("cleaning staging dir: %v", err)
	}
	if n, err := ingest.ResetStaleJobs(conn); err != nil {
		log.Fatalf("resetting stale jobs: %v", err)
	} else if n > 0 {
		log.Printf("returned %d interrupted job(s) to the queue", n)
	}

	// SIGTERM (systemctl stop/restart) не прерывает текущий job: цикл
	// доделывает его и выходит. Жёсткая граница — TimeoutStopSec юнита.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// Без перекодирования job почти целиком — ожидание сети (поиск,
	// скачивание), поэтому несколько job'ов параллельно ускоряют добавление
	// альбома почти линейно даже на 1 vCPU.
	concurrency := 3
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("WORKER_CONCURRENCY must be a positive integer, got %q", v)
		}
		concurrency = n
	}

	log.Printf("worker started (%d parallel), staging dir: %s, storage dir: %s", concurrency, stagingDir, storageDir)

	// ISRC докачиваются отдельно от job'ов: при старте и после каждого
	// готового job'а (сигнал через metadataKick).
	metadataKick := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Лимит Deezer общий с API (тот же IP): API берёт 40 из 50 запросов
		// за 5 с, фоновой докачке хватает и 8.
		dz := deezer.NewClient().WithRateLimit(8, 5*time.Second).WithStore(deezer.NewPGStore(conn))
		for {
			safe.Run("metadata", func() { ingest.FillMetadata(ctx, conn, dz) })
			select {
			case <-ctx.Done():
				return
			case <-metadataKick:
			}
		}
	}()

	keep := audioKeep()
	log.Printf("audio nobody plays for %s is removed from disk", keep)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			safe.Run("housekeep", func() { ingest.Housekeep(conn, storageDir, keep) })
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pollLoop(ctx, conn, stagingDir, storageDir, metadataKick)
		}()
	}
	wg.Wait()
	log.Printf("worker stopped")
}

// pollLoop is deliberately simple — a poll loop over a Postgres-backed queue
// table ("вместо Celery/BullMQ/Temporal — таблица-очередь"). Several run as
// goroutines in this one process; claiming is safe (FOR UPDATE SKIP
// LOCKED), and ResetStaleJobs at startup relies on this being the only
// worker process.
func pollLoop(ctx context.Context, conn *sql.DB, stagingDir, storageDir string, metadataKick chan<- struct{}) {
	const idleDelay = 2 * time.Second
	const errorDelay = 5 * time.Second

	for ctx.Err() == nil {
		job, err := ingest.ClaimNextJob(conn)
		if err != nil {
			log.Printf("claim error: %v", err)
			sleep(ctx, errorDelay)
			continue
		}
		if job == nil {
			sleep(ctx, idleDelay)
			continue
		}
		log.Printf("processing job %d (%s — %s)", job.ID, job.ArtistName, job.Title)
		ingest.ProcessJob(conn, job, stagingDir, storageDir)
		select {
		case metadataKick <- struct{}{}:
		default: // проход уже запланирован
		}
	}
}

// audioKeep — сколько хранить аудио, которое никто не включает
// (AUDIO_KEEP_DAYS, 1–14 дней, по умолчанию 3): потом файл удаляется, а при
// следующем включении трек скачивается заново (ingest.Housekeep).
func audioKeep() time.Duration {
	days := 3
	if v := os.Getenv("AUDIO_KEEP_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("AUDIO_KEEP_DAYS must be a number of days, got %q", v)
		}
		days = max(1, min(n, 14))
	}
	return time.Duration(days) * 24 * time.Hour
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
