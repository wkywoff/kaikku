ALTER TABLE albums ADD COLUMN version_of TEXT;
CREATE INDEX albums_version_of ON albums (version_of) WHERE version_of IS NOT NULL;
