-- Revert the Readarr B2 add-author fields.
ALTER TABLE book_authors DROP COLUMN IF EXISTS monitor_policy;
ALTER TABLE book_authors DROP COLUMN IF EXISTS quality_profile;
ALTER TABLE book_authors DROP COLUMN IF EXISTS metadata_profile;
ALTER TABLE book_authors DROP COLUMN IF EXISTS root_folder;
ALTER TABLE book_authors DROP COLUMN IF EXISTS tags;
