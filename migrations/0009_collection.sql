-- Личная медиатека: лайкнутые треки, сохранённые альбомы, плейлисты.
-- Всё хранится по Deezer ID со снимком метаданных (meta — JSON трека или
-- альбома из ответа API): лайкнуть и добавить в плейлист можно и то, что
-- ещё не скачано (оно скачивается в фоне), а списки строятся без запросов
-- к Deezer. Ручные правки админа накладываются при чтении.
--
-- Старые таблицы из 0001 (playlist_tracks, favorites) ссылались на треки
-- медиатеки и ни разу не использовались — пустые, заменяем.
DROP TABLE playlist_tracks;
DROP TABLE favorites;

ALTER TABLE playlists ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE playlists ADD COLUMN description TEXT NOT NULL DEFAULT '';
-- Публичный плейлист видят и слушают все пользователи сервиса, менять
-- может только владелец.
ALTER TABLE playlists ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE playlists ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE playlists ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE playlist_items (
    id          BIGSERIAL PRIMARY KEY,
    playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    position    DOUBLE PRECISION NOT NULL,
    deezer_id   TEXT NOT NULL,
    meta        JSONB NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_playlist_items ON playlist_items (playlist_id, position);

CREATE TABLE liked_tracks (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id TEXT NOT NULL,
    meta      JSONB NOT NULL,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, deezer_id)
);
CREATE INDEX idx_liked_tracks_recent ON liked_tracks (user_id, added_at DESC);

CREATE TABLE saved_albums (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deezer_id TEXT NOT NULL,
    meta      JSONB NOT NULL,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, deezer_id)
);
CREATE INDEX idx_saved_albums_recent ON saved_albums (user_id, added_at DESC);

-- Раньше главная показывала всё скачанное на сервер, теперь — личную
-- медиатеку. Чтобы она не оказалась пустой, каждому сохраняем альбомы, из
-- которых он сам добавлял хотя бы два трека (так добавлялись альбомы
-- целиком); одиночные прослушивания не переносим.
INSERT INTO saved_albums (user_id, deezer_id, meta, added_at)
SELECT j.requested_by, j.album_deezer_id,
       jsonb_build_object(
           'id', j.album_deezer_id,
           'title', max(j.album_title),
           'artist_id', COALESCE(max(j.album_artist_deezer_id), ''),
           'artist_name', max(j.album_artist_name),
           'cover_url', COALESCE(max(j.cover_url), ''),
           'release_date', COALESCE(to_char(max(j.release_date), 'YYYY-MM-DD'), ''),
           'record_type', COALESCE(max(j.album_record_type), '')),
       min(j.created_at)
  FROM ingest_jobs j
 WHERE j.requested_by IS NOT NULL AND j.album_deezer_id IS NOT NULL AND j.status = 'done'
 GROUP BY j.requested_by, j.album_deezer_id
HAVING count(*) >= 2;
