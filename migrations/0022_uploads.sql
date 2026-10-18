-- Свои релизы (загрузка): артисты, релизы и треки, которых нет в Deezer.
-- Их ID лежат в тех же колонках deezer_id — и в медиатеке, истории, текстах,
-- поэтому лайки, плейлисты и итоги года работают с ними как есть: число —
-- Deezer, 'k' + число из общей последовательности (k1, k2, …) — kaikku.
-- Строки своих релизов помечены edited/cover_custom/picture_custom: воркер
-- их не трогает, а наложение правок (overlay.go) отдаёт их данные.
CREATE SEQUENCE kaikku_ids;

-- Кто загрузил: он и админ правят и удаляют. NULL — данные Deezer.
ALTER TABLE artists ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE albums  ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX albums_owner ON albums (owner_id) WHERE owner_id IS NOT NULL;

-- Черновик своего релиза: видят только автор и админ, в списках его нет
-- (как скрытого админом).
ALTER TABLE albums ADD COLUMN draft BOOLEAN NOT NULL DEFAULT false;
