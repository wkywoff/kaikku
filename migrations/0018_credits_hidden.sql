ALTER TABLE albums ADD COLUMN artist_ids   TEXT[];
ALTER TABLE albums ADD COLUMN credits_from TEXT[];
CREATE INDEX albums_artist_ids ON albums USING gin (artist_ids);

ALTER TABLE albums ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE artists ADD COLUMN picture_url TEXT;

CREATE TABLE track_overrides (
    deezer_id  TEXT PRIMARY KEY,
    explicit   BOOLEAN,
    artist_ids TEXT[],
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE ingest_jobs ADD COLUMN replace_audio BOOLEAN NOT NULL DEFAULT false;
