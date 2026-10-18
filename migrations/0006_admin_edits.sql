-- Ручные правки админа поверх данных Deezer. edited = поля (имя/название,
-- дата, тип) правились руками: воркер их больше не перезаписывает, а API
-- накладывает их поверх ответов Deezer. *_custom = картинку загрузили
-- руками (её не заменит фото/обложка из Deezer).
ALTER TABLE artists ADD COLUMN edited         BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE artists ADD COLUMN picture_custom BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE albums  ADD COLUMN edited         BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE albums  ADD COLUMN cover_custom   BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tracks  ADD COLUMN edited         BOOLEAN NOT NULL DEFAULT false;
