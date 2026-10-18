-- Текст песни — один на запись (ISRC), а не на релиз: сингл и тот же трек
-- в альбоме — разные Deezer ID, и текст искался для каждого отдельно (и
-- мог найтись разный). Уже найденным текстам — ISRC скачанных треков.
ALTER TABLE lyrics ADD COLUMN isrc TEXT NOT NULL DEFAULT '';
UPDATE lyrics l SET isrc = t.isrc FROM tracks t WHERE t.deezer_id = l.deezer_id AND coalesce(t.isrc, '') <> '';
CREATE INDEX idx_lyrics_isrc ON lyrics (isrc) WHERE isrc <> '';
