-- 0014_movie_release_date.up.sql
-- R4: Movie Calendar. Persist the provider release date so the movie
-- calendar (and the "unreleased" status) work without a live TMDB call.
-- "YYYY-MM-DD"; '' when unknown (keyless dev keeps working).
ALTER TABLE movies ADD COLUMN IF NOT EXISTS release_date TEXT NOT NULL DEFAULT '';
