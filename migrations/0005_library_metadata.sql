-- Данные для новых экранов: тип релиза (альбом / EP / сингл / сборник) для
-- фильтров дискографии и медиатеки, фото артистов (скачиваются воркером
-- рядом с обложками) и время добавления для сортировки «недавно добавленные».
ALTER TABLE albums  ADD COLUMN record_type TEXT;
ALTER TABLE albums  ADD COLUMN created_at  TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE artists ADD COLUMN picture_key TEXT;
ALTER TABLE artists ADD COLUMN created_at  TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE tracks  ADD COLUMN created_at  TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE ingest_jobs ADD COLUMN album_record_type TEXT;
