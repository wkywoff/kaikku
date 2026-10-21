-- Диски своего релиза (загрузка) с треками с других релизов: номер диска
-- каждого места в albums.tracklist, по порядку (у своих треков он ещё и в
-- tracks.disc_no, у добавленных — только здесь). NULL — всё на первом диске.
ALTER TABLE albums ADD COLUMN tracklist_discs SMALLINT[];
