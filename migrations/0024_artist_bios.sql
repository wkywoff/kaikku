-- Описания артистов на странице артиста (internal/api/bio.go).
--
-- lang = '' — свой текст (владелец своего артиста или админ): он главнее
-- найденного и на любом языке интерфейса; пустой свой текст — описание
-- скрыто. lang = 'ru' | 'en' — найденное на Deezer или Last.fm, на месяц;
-- source = 'none' — ничего не нашлось (повторный поиск через неделю).
CREATE TABLE artist_bios (
    deezer_id  TEXT NOT NULL,
    lang       TEXT NOT NULL,
    text       TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('custom', 'deezer', 'lastfm', 'none')),
    url        TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (deezer_id, lang)
);
