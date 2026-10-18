-- Аудио, которое давно никто не включал, удаляется с диска (ingest.Housekeep,
-- AUDIO_KEEP_DAYS): строка трека уходит, лайки, плейлисты и история — нет,
-- а при следующем включении трек качается заново с того же видео.
--
-- last_played_at — когда последний раз выдавали ссылку на поток (не чаще
-- раза в час, internal/api). audio_pinned — файл, который админ загрузил
-- вместо найденного на YouTube: его не скачать заново, не удалять. Свои
-- загрузки (k…) и правленые строки Housekeep не трогает и так.
-- known_video_id — видео, с которого трек уже качали: повторная загрузка
-- начинается с него, без поиска.
ALTER TABLE tracks ADD COLUMN last_played_at TIMESTAMPTZ;
ALTER TABLE tracks ADD COLUMN audio_pinned BOOLEAN NOT NULL DEFAULT false;
UPDATE tracks t SET last_played_at = greatest(t.created_at, (SELECT max(p.played_at) FROM plays p WHERE p.deezer_id = t.deezer_id));
ALTER TABLE tracks ALTER COLUMN last_played_at SET DEFAULT now();
ALTER TABLE tracks ALTER COLUMN last_played_at SET NOT NULL;
CREATE INDEX tracks_evictable ON tracks (last_played_at) WHERE NOT audio_pinned;
ALTER TABLE ingest_jobs ADD COLUMN known_video_id TEXT;
