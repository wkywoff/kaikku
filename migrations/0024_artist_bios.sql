CREATE TABLE artist_bios (
    deezer_id  TEXT NOT NULL,
    lang       TEXT NOT NULL,
    text       TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('custom', 'deezer', 'lastfm', 'none')),
    url        TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (deezer_id, lang)
);
