-- Исполнители релиза, заданные админом вместо данных Deezer (Deezer залил
-- релиз не тому артисту — его дубликату): Deezer ID по порядку, первый —
-- основной. credits_from — исполнители релиза у Deezer на момент правки: у
-- треков релиза без своей правки они заменяются на новых. Релиз с правкой
-- попадает в дискографию новых исполнителей и пропадает у старых.
ALTER TABLE albums ADD COLUMN artist_ids   TEXT[];
ALTER TABLE albums ADD COLUMN credits_from TEXT[];
CREATE INDEX albums_artist_ids ON albums USING gin (artist_ids);

-- Скрытый релиз: вне режима админа его нет в дискографии, поиске и
-- подборках; его треки в любимых и плейлистах — серые, недоступные.
ALTER TABLE albums ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT false;

-- Фото артиста у Deezer — для исполнителей, подставленных правкой (их
-- карточки в шапке альбома).
ALTER TABLE artists ADD COLUMN picture_url TEXT;

-- Правки трека, которым не важно, скачан ли он: метка explicit и
-- исполнители (NULL — как у Deezer).
CREATE TABLE track_overrides (
    deezer_id  TEXT PRIMARY KEY,
    explicit   BOOLEAN,
    artist_ids TEXT[],
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Перекачать аудио трека, который уже есть в медиатеке (админ): воркер не
-- берёт файл той же записи с другого релиза, а заменяет файл в строке трека.
ALTER TABLE ingest_jobs ADD COLUMN replace_audio BOOLEAN NOT NULL DEFAULT false;
