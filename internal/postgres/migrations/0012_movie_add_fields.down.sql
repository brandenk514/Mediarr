-- Revert the R2 add-flow fields.
ALTER TABLE movies DROP COLUMN IF EXISTS minimum_availability;
ALTER TABLE movies DROP COLUMN IF EXISTS root_folder;
ALTER TABLE movies DROP COLUMN IF EXISTS tags;
