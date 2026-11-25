ALTER TABLE ingest_jobs ADD COLUMN attempts INT NOT NULL DEFAULT 0;
ALTER TABLE ingest_jobs ADD COLUMN manual_video_id TEXT;
UPDATE ingest_jobs SET attempts = 1 WHERE status = 'failed';
CREATE INDEX ingest_jobs_queue ON ingest_jobs (attempts, id) WHERE status = 'pending';
CREATE INDEX ingest_jobs_failed ON ingest_jobs (updated_at) WHERE status = 'failed';
