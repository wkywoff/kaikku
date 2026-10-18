-- История прослушиваний и подписки на артистов.
--
-- plays — трек, дослушанный хотя бы до 30 секунд (или до половины, если он
-- короче), со снимком метаданных (как в liked_tracks) и тем, откуда его
-- включили: альбом, плейлист, артист или «Любимые». Из неё строятся
-- «Недавно прослушанное» и подсказки «Тебе может понравиться».
CREATE TABLE plays (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id    TEXT NOT NULL,
    meta         JSONB NOT NULL,
    context_kind TEXT NOT NULL CHECK (context_kind IN ('album', 'playlist', 'artist', 'liked')),
    context_id   TEXT NOT NULL,
    -- Для артиста — его снимок (имя, фото), чтобы список недавнего не ходил в Deezer.
    context_meta JSONB,
    played_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_plays_recent ON plays (user_id, played_at DESC);

-- Подписки: новые релизы этих артистов показываются на главной.
CREATE TABLE followed_artists (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id TEXT NOT NULL,
    meta      JSONB NOT NULL,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, deezer_id)
);
