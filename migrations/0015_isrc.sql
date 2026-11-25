ALTER TABLE ingest_jobs ADD COLUMN isrc TEXT;
ALTER TABLE tracks ADD COLUMN isrc TEXT;
CREATE INDEX idx_tracks_isrc ON tracks (isrc) WHERE isrc <> '';
