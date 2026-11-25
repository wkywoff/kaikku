CREATE TABLE deezer_cache (
    key        TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX deezer_cache_expires ON deezer_cache (expires_at);
