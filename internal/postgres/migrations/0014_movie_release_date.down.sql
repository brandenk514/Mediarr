-- Revert the R4 release-date column.
ALTER TABLE movies DROP COLUMN IF EXISTS release_date;
