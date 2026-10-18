-- Версии релиза (админ): делюкс, clean и т. п. привязываются к основному
-- (его Deezer ID). Привязанная версия пропадает из дискографии, поиска и
-- новинок и показывается на странице основного в «Других версиях».
ALTER TABLE albums ADD COLUMN version_of TEXT;
CREATE INDEX albums_version_of ON albums (version_of) WHERE version_of IS NOT NULL;
