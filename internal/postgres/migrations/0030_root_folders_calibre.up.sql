-- Readarr B5: per-root Calibre Content Server config (JSON; {} = not
-- configured / disabled default). The password inside is masked on read by
-- the API (never echoed) and kept from the stored row on write.
ALTER TABLE root_folders ADD COLUMN IF NOT EXISTS calibre JSONB NOT NULL DEFAULT '{}'::jsonb;
