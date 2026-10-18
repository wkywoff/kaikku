-- Подписки между людьми (internal/api/profiles.go): взаимная подписка —
-- «друзья», только их владелец зовёт в соавторы плейлиста.
CREATE TABLE user_follows (
    follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);
CREATE INDEX user_follows_followee ON user_follows (followee_id);
