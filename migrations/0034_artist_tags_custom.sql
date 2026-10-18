-- Свои жанры артиста (internal/api/tags.go): поставил владелец своего
-- артиста или админ. Фоновый проход по Last.fm их не трогает.
ALTER TABLE artist_tags ADD COLUMN custom BOOLEAN NOT NULL DEFAULT false;
