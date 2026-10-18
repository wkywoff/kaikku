-- Теги артистов с Last.fm (internal/api/tags.go): жанры на странице
-- артиста и для миксов (mixes.go). tags — [{"name": "phonk", "count": 100}, …]
-- по убыванию веса (count — вес у этого артиста на Last.fm, 0–100), уже
-- очищенные от «seen live» и т.п.; пустой массив — у Last.fm тегов нет
-- (спросим снова через неделю). Обновляются раз в месяц.
CREATE TABLE artist_tags (
    deezer_id  TEXT PRIMARY KEY,
    tags       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

