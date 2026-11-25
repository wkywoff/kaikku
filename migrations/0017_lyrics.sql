CREATE TABLE lyrics (
    deezer_id  TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    sync       TEXT NOT NULL,
    body       JSONB,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
