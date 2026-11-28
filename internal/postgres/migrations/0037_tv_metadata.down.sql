-- 0037_tv_metadata.down.sql

ALTER TABLE tv_series DROP COLUMN IF EXISTS genres;
ALTER TABLE tv_series DROP COLUMN IF EXISTS keywords;
ALTER TABLE tv_series DROP COLUMN IF EXISTS original_language;
ALTER TABLE tv_series DROP COLUMN IF EXISTS status;
ALTER TABLE tv_series DROP COLUMN IF EXISTS network;
ALTER TABLE tv_series DROP COLUMN IF EXISTS first_air_date;
ALTER TABLE tv_series DROP COLUMN IF EXISTS production_countries;
ALTER TABLE tv_series DROP COLUMN IF EXISTS tmdb_pct;
