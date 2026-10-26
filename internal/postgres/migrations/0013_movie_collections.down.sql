-- Revert the R3 collection fields.
ALTER TABLE movies DROP COLUMN IF EXISTS collection_name;
ALTER TABLE movies DROP COLUMN IF EXISTS collection_id;
