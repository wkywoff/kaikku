-- Каталог идентифицируется по Deezer ID, а не по имени: разные исполнители
-- с одинаковым именем больше не склеиваются, а альбом группируется по своему
-- альбомному исполнителю (сборники, фиты). Строки, созданные до этой
-- миграции, получают deezer_id при первом совпадении по имени (см.
-- internal/ingest/catalog.go).

ALTER TABLE artists DROP CONSTRAINT artists_name_key;
ALTER TABLE artists ADD COLUMN deezer_id TEXT UNIQUE;
CREATE INDEX idx_artists_name ON artists (name);

ALTER TABLE albums DROP CONSTRAINT albums_artist_id_title_key;
ALTER TABLE albums ADD COLUMN deezer_id TEXT UNIQUE;

ALTER TABLE tracks ADD COLUMN deezer_id TEXT UNIQUE;
ALTER TABLE tracks ADD COLUMN disc_no INTEGER;

-- Удаление трека не должно падать из-за ссылки из истории ingest'а; job с
-- обнулённым result_track_id можно поставить в очередь заново.
ALTER TABLE ingest_jobs DROP CONSTRAINT ingest_jobs_result_track_id_fkey;
ALTER TABLE ingest_jobs ADD CONSTRAINT ingest_jobs_result_track_id_fkey
    FOREIGN KEY (result_track_id) REFERENCES tracks(id) ON DELETE SET NULL;

ALTER TABLE ingest_jobs ADD COLUMN artist_deezer_id       TEXT;
ALTER TABLE ingest_jobs ADD COLUMN album_artist_deezer_id TEXT;
ALTER TABLE ingest_jobs ADD COLUMN album_deezer_id        TEXT;
ALTER TABLE ingest_jobs ADD COLUMN disc_no                INTEGER;
ALTER TABLE ingest_jobs ADD COLUMN release_date           DATE;
ALTER TABLE ingest_jobs ADD COLUMN video_id               TEXT; -- какой ролик YouTube выбран матчингом
