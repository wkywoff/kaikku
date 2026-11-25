ALTER TABLE tracks ADD COLUMN last_played_at TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN audio_pinned BOOLEAN NOT NULL DEFAULT false;
UPDATE tracks t SET last_played_at = greatest(t.created_at, (SELECT max(p.played_at) FROM plays p WHERE p.deezer_id = t.deezer_id));
ALTER TABLE tracks ALTER COLUMN last_played_at SET DEFAULT now();
ALTER TABLE tracks ALTER COLUMN last_played_at SET NOT NULL;
CREATE INDEX tracks_evictable ON tracks (last_played_at) WHERE NOT audio_pinned;
ALTER TABLE ingest_jobs ADD COLUMN known_video_id TEXT;
