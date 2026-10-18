-- ISRC — код самой записи. Одна запись на разных релизах (сингл, альбом,
-- делюкс) у Deezer — разные треки с разными ID, но ISRC у них один. По нему
-- воркер берёт уже скачанный файл, а не качает ту же запись заново (строки
-- tracks тогда делят storage_key), а итоги года считают её одной песней.
ALTER TABLE ingest_jobs ADD COLUMN isrc TEXT;
-- NULL — ещё не узнавали у Deezer (треки до этой миграции дозаполняет
-- воркер, metadata.go), '' — у Deezer его нет.
ALTER TABLE tracks ADD COLUMN isrc TEXT;
CREATE INDEX idx_tracks_isrc ON tracks (isrc) WHERE isrc <> '';
