-- Совместные плейлисты (internal/api/playlist_collab.go): владелец зовёт
-- соавторов — они добавляют, убирают и переставляют треки; название,
-- обложку и удаление плейлиста меняет только владелец. added_by — кто
-- добавил трек (у прежних — владелец плейлиста).
CREATE TABLE playlist_collaborators (
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (playlist_id, user_id)
);
CREATE INDEX playlist_collaborators_user ON playlist_collaborators (user_id);

ALTER TABLE playlist_items ADD COLUMN added_by UUID REFERENCES users(id) ON DELETE SET NULL;
UPDATE playlist_items i SET added_by = p.user_id FROM playlists p WHERE p.id = i.playlist_id;
