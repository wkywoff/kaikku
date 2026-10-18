-- Тексты песен (internal/lyrics): что нашли и где, чтобы не спрашивать
-- источники при каждом открытии. body NULL — текста нигде нет (повторим
-- поиск через неделю).
CREATE TABLE lyrics (
    deezer_id  TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    sync       TEXT NOT NULL,
    body       JSONB,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
