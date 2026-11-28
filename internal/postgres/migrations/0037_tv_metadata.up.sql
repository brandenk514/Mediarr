-- 0037_tv_metadata.up.sql
-- #157: persist the TMDB series enrichment the detail fetch reports
-- (genres, keywords, original language, status, network, first air date,
-- production countries, TMDB user score) so list reads serve the values
-- without a per-row provider call (the #150 movie pattern). Zero/empty =
-- unknown (the keyless convention: existing rows degrade to the
-- defaults, never an error). Arrays follow the Tags convention (TEXT[]).

ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS genres               TEXT[]  NOT NULL DEFAULT '{}';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS keywords             TEXT[]  NOT NULL DEFAULT '{}';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS original_language    TEXT    NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS status               TEXT    NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS network              TEXT    NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS first_air_date       TEXT    NOT NULL DEFAULT '';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS production_countries TEXT[]  NOT NULL DEFAULT '{}';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS tmdb_pct             REAL    NOT NULL DEFAULT 0;
