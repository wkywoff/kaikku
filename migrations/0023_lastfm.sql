CREATE TABLE lastfm_links (
    user_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    username    TEXT NOT NULL,
    session_key TEXT NOT NULL,
    scrobble    BOOLEAN NOT NULL DEFAULT true,
    love        BOOLEAN NOT NULL DEFAULT true,
    linked_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE lastfm_queue (
    id        BIGSERIAL PRIMARY KEY,
    user_id   UUID NOT NULL REFERENCES lastfm_links(user_id) ON DELETE CASCADE,
    artist    TEXT NOT NULL,
    track     TEXT NOT NULL,
    album     TEXT NOT NULL DEFAULT '',
    duration  INT NOT NULL DEFAULT 0,
    played_at TIMESTAMPTZ NOT NULL,
    attempts  INT NOT NULL DEFAULT 0,
    next_try  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lastfm_queue_due ON lastfm_queue (next_try);
