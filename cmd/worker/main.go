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
	"kaikku/internal/ytdlp"
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
	stagingDir := os.Getenv("STAGING_DIR")
	if stagingDir == "" {
		stagingDir = "./data/staging"
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		log.Fatalf("staging dir: %v", err)
	}
	if err := ytdlp.SetProxy(os.Getenv("YTDLP_PROXY")); err != nil {
		log.Fatalf("YTDLP_PROXY: %v", err)
	}
	ytdlp.SetCookiesFile(ytdlp.CookiesPath(stagingDir))

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	concurrency := 3
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("WORKER_CONCURRENCY must be a positive integer, got %q", v)
		}
		concurrency = n
	}

	log.Printf("worker started (%d parallel), staging dir: %s, storage dir: %s", concurrency, stagingDir, storageDir)

	metadataKick := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
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

func pollLoop(ctx context.Context, conn *sql.DB, stagingDir, storageDir string, metadataKick chan<- struct{}) {
	const idleDelay = 2 * time.Second
	const errorDelay = 5 * time.Second
	const lowDiskDelay = time.Minute

	for ctx.Err() == nil {
		if ingest.DiskLow(storageDir) {
			log.Printf("under 1 GB free in %s: jobs wait", storageDir)
			sleep(ctx, lowDiskDelay)
			continue
		}
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
		default:
		}
	}
}

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
