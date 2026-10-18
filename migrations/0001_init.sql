-- Требует PostgreSQL 13+ (gen_random_uuid() встроена в ядро с 13-й версии,
-- отдельного pgcrypto не нужно).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE artists (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,
    search_vector TSVECTOR
);
CREATE INDEX idx_artists_search ON artists USING gin (search_vector);
CREATE INDEX idx_artists_name_trgm ON artists USING gin (name gin_trgm_ops);

CREATE TABLE albums (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artist_id   UUID REFERENCES artists(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    cover_key   TEXT,
    released_at DATE,
    UNIQUE (artist_id, title)
);
CREATE INDEX idx_albums_artist ON albums (artist_id);

CREATE TABLE tracks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    album_id      UUID REFERENCES albums(id) ON DELETE CASCADE,
    artist_id     UUID REFERENCES artists(id), -- per-track исполнитель (сборники/фиты); NULL -> UI берёт album.artist_id
    title         TEXT NOT NULL,
    duration_ms   INTEGER NOT NULL,
    track_no      INTEGER,
    storage_key   TEXT NOT NULL,
    loudness_lufs REAL,
    search_vector TSVECTOR
);
CREATE INDEX idx_tracks_album ON tracks (album_id);
CREATE INDEX idx_tracks_artist ON tracks (artist_id);
CREATE INDEX idx_tracks_search ON tracks USING gin (search_vector);
CREATE INDEX idx_tracks_title_trgm ON tracks USING gin (title gin_trgm_ops);

CREATE TABLE playlists (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    title   TEXT NOT NULL
);
CREATE INDEX idx_playlists_user ON playlists (user_id);

CREATE TABLE playlist_tracks (
    playlist_id UUID REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    UUID REFERENCES tracks(id) ON DELETE CASCADE,
    position    DOUBLE PRECISION NOT NULL, -- дробные позиции: вставка без reindex всего плейлиста
    PRIMARY KEY (playlist_id, track_id)
);

CREATE TABLE favorites (
    user_id  UUID REFERENCES users(id) ON DELETE CASCADE,
    track_id UUID REFERENCES tracks(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, track_id)
);

CREATE TABLE play_history (
    id        BIGSERIAL PRIMARY KEY,
    user_id   UUID REFERENCES users(id) ON DELETE CASCADE,
    track_id  UUID REFERENCES tracks(id) ON DELETE CASCADE,
    played_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_play_history_user ON play_history (user_id, played_at DESC);

-- ingest_jobs — очередь "найти на YouTube и скачать". source_ref — дедуп ДО
-- обращения к Deezer/yt-dlp; метаданные резолвятся из Deezer в момент
-- постановки в очередь и используются воркером напрямую.
CREATE TABLE ingest_jobs (
    id                BIGSERIAL PRIMARY KEY,
    requested_by      UUID REFERENCES users(id),
    source_ref        TEXT NOT NULL UNIQUE,             -- напр. 'deezer:3135556'
    title             TEXT NOT NULL,
    artist_name       TEXT NOT NULL,
    album_artist_name TEXT NOT NULL,                    -- отдельно от artist_name — важно для сборников/фитов
    album_title       TEXT NOT NULL,
    track_no          INTEGER,
    duration_ms       INTEGER NOT NULL,                 -- эталонная длительность для матчинга с YouTube
    cover_url         TEXT,                              -- обложка из Deezer — скачивается воркером
    status            TEXT NOT NULL DEFAULT 'pending',   -- pending/processing/done/failed
    error             TEXT,
    result_track_id   UUID REFERENCES tracks(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- search_vector — конфиг 'simple' (без стемминга/стоп-слов), см. design doc §7:
-- мультиязычный каталог + короткие тайтлы-омонимы стоп-слов делают
-- языкоспецифичные конфиги контрпродуктивными здесь.
CREATE OR REPLACE FUNCTION tracks_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('simple', NEW.title);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_tracks_search_vector
    BEFORE INSERT OR UPDATE OF title ON tracks
    FOR EACH ROW EXECUTE FUNCTION tracks_search_vector_update();

CREATE OR REPLACE FUNCTION artists_search_vector_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('simple', NEW.name);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_artists_search_vector
    BEFORE INSERT OR UPDATE OF name ON artists
    FOR EACH ROW EXECUTE FUNCTION artists_search_vector_update();
