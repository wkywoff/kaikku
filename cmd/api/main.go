package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"kaikku/internal/api"
	"kaikku/internal/db"
	"kaikku/internal/deezer"
	"kaikku/internal/lastfm"
	"kaikku/internal/media"
	"kaikku/internal/ytdlp"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kaikku:kaikku@localhost:5432/kaikku?sslmode=disable"
	}

	jwtSecret, err := loadJWTSecret()
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}

	signer, err := loadMediaSigner()
	if err != nil {
		log.Fatalf("media signer: %v", err)
	}
	// Адрес сайта — для полных ссылок в превью и картинок в приложениях.
	// Треки играют и без него: ссылки на аудио относительные.
	if os.Getenv("MEDIA_BASE_URL") == "" {
		log.Printf("MEDIA_BASE_URL is not set: link previews and artwork in apps need the site's address")
	}

	conn, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	streams, err := loadStreamHub()
	if err != nil {
		log.Fatalf("stream hub: %v", err)
	}

	deezerCache := deezer.NewPGStore(conn)
	dz := deezer.NewClient().WithStore(deezerCache)
	// Второй выход к Deezer (другой IP — вдвое больше лимит): прокси, например
	// WARP как локальный SOCKS5.
	if p := os.Getenv("DEEZER_PROXY"); p != "" {
		u, err := url.Parse(p)
		if err != nil || u.Host == "" {
			log.Fatalf("DEEZER_PROXY: want socks5://host:port, got %q", p)
		}
		dz.WithProxy(u, 40, 5*time.Second)
		log.Printf("deezer: second egress via %s", u.Host)
	}
	// Тот же каталог, что у воркера (и то же значение по умолчанию).
	storageDir := os.Getenv("STORAGE_DIR")
	if storageDir == "" {
		storageDir = "./data/storage"
	}
	srv := &api.Server{
		DB:          conn,
		JWTSecret:   jwtSecret,
		MediaSigner: signer,
		Deezer:      dz,
		DeezerCache: deezerCache,
		DeezerSite:  deezer.NewSite(),
		StorageDir:  storageDir,
		Streams:     streams,
		// Скробблинг: без ключа приложения Last.fm выключен.
		Lastfm: lastfm.New(os.Getenv("LASTFM_API_KEY"), os.Getenv("LASTFM_API_SECRET")),
		// Превью ссылок: index.html фронта (в Docker — /app/frontend/index.html).
		FrontendIndex: os.Getenv("FRONTEND_INDEX"),
		PublicURL:     os.Getenv("MEDIA_BASE_URL"),
	}
	srv.EnableLyrics()
	srv.SweepUploads()
	handler := api.NewRouter(srv)

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	// Ответ — до минуты, как ждёт nginx (proxy_read_timeout по умолчанию):
	// добавление сотни треков в плейлист или страница при занятом лимите
	// Deezer (40 запросов за 5 с) идут дольше 15 с, и раньше такой ответ
	// обрывался, хотя всё уже сохранилось. Что идёт ещё дольше — поток и
	// загрузка аудио — продлевает себе срок само.
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Деплой перезапускает API (SIGTERM): начатые запросы доделываются, а
	// не обрываются на полуслове. Потоки музыки длинные — через 5 с их
	// закрываем, плеер перезапросит ссылку.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	srv.StartLastfm(ctx)
	srv.StartTagger(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-stopped // ListenAndServe возвращается сразу, а Shutdown ещё ждёт запросы
	log.Printf("stopped")
}

// loadStreamHub: STAGING_DIR — тот же, что у воркера (туда API кладёт
// скачанное при потоковом воспроизведении); STREAM_DIR — рабочий каталог
// API, по умолчанию рядом со staging (та же ФС — передача hard link'ом, и
// он внутри ReadWritePaths юнита).
func loadStreamHub() (*api.StreamHub, error) {
	stagingDir := os.Getenv("STAGING_DIR")
	if stagingDir == "" {
		stagingDir = "./data/staging"
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, err
	}
	streamDir := os.Getenv("STREAM_DIR")
	if streamDir == "" {
		streamDir = filepath.Join(filepath.Dir(filepath.Clean(stagingDir)), "stream")
	}
	hub, err := api.NewStreamHub(streamDir, stagingDir)
	if err != nil {
		return nil, err
	}
	// Поиск и извлечение для потока — через постоянно запущенный yt-dlp
	// (минус ~1 с до первого звука); воркер запускает yt-dlp как раньше.
	ytdlp.EnableWarm()
	return hub, nil
}

func loadJWTSecret() ([]byte, error) {
	hexSecret := os.Getenv("JWT_SECRET")
	if hexSecret == "" {
		log.Println("WARNING: JWT_SECRET not set — generating an ephemeral one; " +
			"every restart invalidates all existing tokens. Set JWT_SECRET " +
			"(64 hex chars, e.g. `openssl rand -hex 32`) before real use.")
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		return secret, nil
	}
	return hex.DecodeString(hexSecret)
}

func loadMediaSigner() (*media.Signer, error) {
	secret := os.Getenv("MEDIA_SECRET")
	if secret == "" {
		// Unlike JWT_SECRET, no ephemeral fallback makes sense here: this
		// exact string also has to be hardcoded in nginx's secure_link_md5
		// directive (docker/nginx.conf.template), so it must be stable and
		// explicitly chosen, not regenerated per process restart.
		return nil, fmt.Errorf("MEDIA_SECRET is required (must match nginx's secure_link_md5 secret, see docker/nginx.conf.template)")
	}
	ttlSeconds := 600 // 10 минут: ссылка на трек живёт, пока он играет, и перевыпускается плеером
	if v := os.Getenv("MEDIA_TTL_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("MEDIA_TTL_SECONDS must be an integer: %w", err)
		}
		ttlSeconds = n
	}
	return &media.Signer{Secret: secret, TTL: time.Duration(ttlSeconds) * time.Second}, nil
}
