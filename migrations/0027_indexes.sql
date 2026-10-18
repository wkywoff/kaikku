-- Индексы для частых запросов, которые шли полным проходом:
-- tracks.storage_key — замена и удаление аудио (кто ещё делит файл),
-- plays по контексту — снимок артиста при записи прослушивания
-- (resolvePlayContext) по всей истории сервиса.
CREATE INDEX tracks_storage_key ON tracks (storage_key);
CREATE INDEX plays_context ON plays (context_kind, context_id, id DESC);
