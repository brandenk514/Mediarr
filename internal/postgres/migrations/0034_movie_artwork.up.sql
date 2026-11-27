-- 0034_movie_artwork.up.sql
-- #149 Stage A: persist the upstream poster/backdrop URLs the TMDB enricher
-- reports, so list reads serve PosterURL without a per-row provider call.
-- Both are '' when unknown (keyless dev keeps working; existing rows
-- degrade to '').

ALTER TABLE movies ADD COLUMN IF NOT EXISTS poster_url   TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS backdrop_url TEXT NOT NULL DEFAULT '';
