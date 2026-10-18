-- Чистка после перехода на каталог Deezer.

-- Поиск идёт через Deezer, а не по своей базе: полнотекстовые колонки,
-- триггеры к ним и trgm-индексы из 0001 ничем не используются.
DROP TRIGGER trg_tracks_search_vector ON tracks;
DROP TRIGGER trg_artists_search_vector ON artists;
DROP FUNCTION tracks_search_vector_update();
DROP FUNCTION artists_search_vector_update();
ALTER TABLE tracks DROP COLUMN search_vector;
ALTER TABLE artists DROP COLUMN search_vector;
DROP INDEX idx_artists_name_trgm;
DROP INDEX idx_tracks_title_trgm;
-- Искал артиста по имени для строк без deezer_id (catalog.go до этой миграции).
DROP INDEX idx_artists_name;

-- История прослушиваний живёт в plays (0011); эта таблица из 0001 пустая.
DROP TABLE play_history;

-- Каталог — это Deezer: каждая запись заводится по Deezer ID (воркер,
-- «claim» админа). Строк без него нет, и появиться им неоткуда.
ALTER TABLE artists ALTER COLUMN deezer_id SET NOT NULL;
ALTER TABLE albums ALTER COLUMN deezer_id SET NOT NULL;
ALTER TABLE tracks ALTER COLUMN deezer_id SET NOT NULL;

-- Удалить пользователя, который что-то скачивал, не давал внешний ключ без
-- ON DELETE; задания остаются, просто без автора.
ALTER TABLE ingest_jobs DROP CONSTRAINT ingest_jobs_requested_by_fkey;
ALTER TABLE ingest_jobs ADD CONSTRAINT ingest_jobs_requested_by_fkey
    FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE SET NULL;

-- Когда воркер в последний раз спрашивал у Deezer фото артиста, которого
-- там нет: раньше такой артист перезапрашивался после каждого трека.
ALTER TABLE artists ADD COLUMN picture_checked_at TIMESTAMPTZ;
