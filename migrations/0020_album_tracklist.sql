-- Свой треклист релиза (админ): порядок треков и треки с других релизов
-- вместо его собственных (Deezer залил не ту версию — подставляется нужная
-- по ссылке). Deezer ID по порядку; NULL — как у Deezer. tracklist_from —
-- треклист Deezer на момент правки: что Deezer добавит позже, встанет в конец.
ALTER TABLE albums ADD COLUMN tracklist      TEXT[];
ALTER TABLE albums ADD COLUMN tracklist_from TEXT[];
