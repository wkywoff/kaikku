CREATE TABLE artist_tags (
    deezer_id  TEXT PRIMARY KEY,
    tags       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

