-- Revert the #149 Stage A artwork fields.
ALTER TABLE movies DROP COLUMN IF EXISTS backdrop_url;
ALTER TABLE movies DROP COLUMN IF EXISTS poster_url;
