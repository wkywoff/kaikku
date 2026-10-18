-- Кеш ответов Deezer, который переживает перезапуск (internal/deezer/pgstore.go):
-- треки, альбомы, треклисты, страницы артистов. Устаревшее удаляет
-- уборка воркера (ingest.Housekeep).
CREATE TABLE deezer_cache (
    key        TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX deezer_cache_expires ON deezer_cache (expires_at);
