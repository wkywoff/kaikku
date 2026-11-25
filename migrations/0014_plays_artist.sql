CREATE INDEX idx_plays_artist ON plays ((meta->>'artist_id'), played_at);
