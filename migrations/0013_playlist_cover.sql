-- Своя обложка плейлиста (загружает владелец): ключ файла в STORAGE_DIR,
-- covers/custom/<hex>.jpg — как у картинок, загруженных админом. NULL —
-- обложкой служит мозаика из обложек треков.
ALTER TABLE playlists ADD COLUMN cover_key TEXT;
