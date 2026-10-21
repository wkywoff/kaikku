-- Метка explicit трека в Deezer: по ней поиск на YouTube Music выбирает
-- версию с матом или clean (у них одинаковые название, длина и альбом).
-- NULL — задачи до этого поля: без предпочтения.
ALTER TABLE ingest_jobs ADD COLUMN explicit BOOLEAN;
