CREATE TABLE plays (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id    TEXT NOT NULL,
    meta         JSONB NOT NULL,
    context_kind TEXT NOT NULL CHECK (context_kind IN ('album', 'playlist', 'artist', 'liked')),
    context_id   TEXT NOT NULL,
    context_meta JSONB,
    played_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_plays_recent ON plays (user_id, played_at DESC);

CREATE TABLE followed_artists (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id TEXT NOT NULL,
    meta      JSONB NOT NULL,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, deezer_id)
);
