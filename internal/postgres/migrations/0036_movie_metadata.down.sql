-- Revert the #150 movie metadata fields.
ALTER TABLE movies DROP COLUMN IF EXISTS tmdb_pct;
ALTER TABLE movies DROP COLUMN IF EXISTS certification;
ALTER TABLE movies DROP COLUMN IF EXISTS original_language;
ALTER TABLE movies DROP COLUMN IF EXISTS runtime;
ALTER TABLE movies DROP COLUMN IF EXISTS genres;
