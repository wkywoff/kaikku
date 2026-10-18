-- Включённое из микса (internal/api/mixes.go) — тоже место в истории:
-- «Недавно прослушанное» показывает карточку микса.
ALTER TABLE plays DROP CONSTRAINT plays_context_kind_check;
ALTER TABLE plays ADD CONSTRAINT plays_context_kind_check
    CHECK (context_kind IN ('album', 'playlist', 'artist', 'liked', 'mix'));
