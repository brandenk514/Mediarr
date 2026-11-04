-- Readarr B2 (Add author/book flow) — the author add-flow fields.
-- Additive: existing rows keep "" (empty) = "all" / "none" and no tags.
ALTER TABLE book_authors ADD COLUMN IF NOT EXISTS monitor_policy   TEXT NOT NULL DEFAULT '';
ALTER TABLE book_authors ADD COLUMN IF NOT EXISTS quality_profile  TEXT NOT NULL DEFAULT '';
ALTER TABLE book_authors ADD COLUMN IF NOT EXISTS metadata_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE book_authors ADD COLUMN IF NOT EXISTS root_folder      TEXT NOT NULL DEFAULT '';
ALTER TABLE book_authors ADD COLUMN IF NOT EXISTS tags             TEXT[] NOT NULL DEFAULT '{}';
