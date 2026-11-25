ALTER TABLE ingest_jobs ADD COLUMN other_artists TEXT[] NOT NULL DEFAULT '{}';
