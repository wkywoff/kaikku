ALTER TABLE plays DROP CONSTRAINT plays_context_kind_check;
ALTER TABLE plays ADD CONSTRAINT plays_context_kind_check
    CHECK (context_kind IN ('album', 'playlist', 'artist', 'liked', 'mix'));
