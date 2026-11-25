CREATE TABLE discography_extras (
    artist_id  TEXT NOT NULL,
    album_id   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (artist_id, album_id)
);
