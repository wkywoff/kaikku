DROP TRIGGER trg_tracks_search_vector ON tracks;
DROP TRIGGER trg_artists_search_vector ON artists;
DROP FUNCTION tracks_search_vector_update();
DROP FUNCTION artists_search_vector_update();
ALTER TABLE tracks DROP COLUMN search_vector;
ALTER TABLE artists DROP COLUMN search_vector;
DROP INDEX idx_artists_name_trgm;
DROP INDEX idx_tracks_title_trgm;
DROP INDEX idx_artists_name;

DROP TABLE play_history;

ALTER TABLE artists ALTER COLUMN deezer_id SET NOT NULL;
ALTER TABLE albums ALTER COLUMN deezer_id SET NOT NULL;
ALTER TABLE tracks ALTER COLUMN deezer_id SET NOT NULL;

ALTER TABLE ingest_jobs DROP CONSTRAINT ingest_jobs_requested_by_fkey;
ALTER TABLE ingest_jobs ADD CONSTRAINT ingest_jobs_requested_by_fkey
    FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE artists ADD COLUMN picture_checked_at TIMESTAMPTZ;
