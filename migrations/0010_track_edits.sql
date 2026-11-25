CREATE TABLE track_edits (
    deezer_id  TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    track_no   INTEGER,
    disc_no    INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
