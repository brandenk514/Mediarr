-- 0013_movie_collections.up.sql
-- R3: Collections + Discover (Radarr-specific). Persist the TMDB collection a
-- movie belongs to so the collections page can group the library without a
-- live TMDB call. Both are '' when unknown (keyless dev keeps working).

ALTER TABLE movies ADD COLUMN IF NOT EXISTS collection_id   TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS collection_name TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_movies_collection_id ON movies(collection_id);
