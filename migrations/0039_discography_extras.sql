-- Релизы, которых нет в списке релизов артиста у Deezer (свежий альбом он
-- вносит туда с опозданием на дни), но страницу которых кто-то открыл:
-- дискография добавляет их к ответу Deezer (creditedDiscography в
-- internal/api/overlay.go).
CREATE TABLE discography_extras (
    artist_id  TEXT NOT NULL,
    album_id   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (artist_id, album_id)
);
