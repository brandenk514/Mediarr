-- 0036_movie_metadata.up.sql
-- #150: persist the TMDB enrichment metadata the detail fetch already
-- reports (genres, runtime, original language, certification, TMDB user
-- score) so the per-kind list endpoint can filter and sort on it without
-- a per-row provider call. Zero/empty = unknown (the keyless convention:
-- existing rows degrade to the defaults, never an error).
-- Genres follow the Tags convention (0012: TEXT[]).

ALTER TABLE movies ADD COLUMN IF NOT EXISTS genres            TEXT[]  NOT NULL DEFAULT '{}';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS runtime           INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movies ADD COLUMN IF NOT EXISTS original_language  TEXT    NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS certification     TEXT    NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS tmdb_pct          REAL    NOT NULL DEFAULT 0;
