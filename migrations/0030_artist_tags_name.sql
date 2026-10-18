-- Страницы жанров (internal/api/genres.go): артистов жанра показываем и
-- тем, кто их не слушал, — нужно имя (под ним их нашли на Last.fm), а
-- искать по тегу — индекс.
ALTER TABLE artist_tags ADD COLUMN name TEXT;
UPDATE artist_tags t SET name = COALESCE(
    (SELECT max(f.meta->>'name') FROM followed_artists f WHERE f.deezer_id = t.deezer_id),
    (SELECT max(p.meta->>'artist') FROM plays p WHERE p.meta->>'artist_id' = t.deezer_id),
    (SELECT max(l.meta->>'artist') FROM liked_tracks l WHERE l.meta->>'artist_id' = t.deezer_id));
CREATE INDEX artist_tags_tags ON artist_tags USING gin (tags jsonb_path_ops);
