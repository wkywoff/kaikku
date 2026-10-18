-- Админские правки треков, которых ещё нет в медиатеке: по Deezer ID, без
-- скачивания (строке tracks нужен аудиофайл). Накладываются на ответы
-- Deezer так же, как правки строк tracks; когда трек скачается, воркер
-- переносит правку в его строку и удаляет отсюда.
CREATE TABLE track_edits (
    deezer_id  TEXT PRIMARY KEY,
    title      TEXT NOT NULL,
    track_no   INTEGER,
    disc_no    INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
