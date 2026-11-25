ALTER TABLE lyrics ADD COLUMN isrc TEXT NOT NULL DEFAULT '';
UPDATE lyrics l SET isrc = t.isrc FROM tracks t WHERE t.deezer_id = l.deezer_id AND coalesce(t.isrc, '') <> '';
CREATE INDEX idx_lyrics_isrc ON lyrics (isrc) WHERE isrc <> '';
