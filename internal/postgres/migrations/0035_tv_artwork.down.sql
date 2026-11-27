-- Revert the #149 Stage A TV artwork fields.
ALTER TABLE tv_series DROP COLUMN IF EXISTS backdrop_url;
ALTER TABLE tv_series DROP COLUMN IF EXISTS poster_url;
