CREATE INDEX tracks_storage_key ON tracks (storage_key);
CREATE INDEX plays_context ON plays (context_kind, context_id, id DESC);
