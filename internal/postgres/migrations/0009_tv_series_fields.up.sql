-- TV series parity fields (P2): series_type, season_folder, tags.
-- Additive/backward-compatible: existing rows get sensible defaults.
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS series_type   TEXT   NOT NULL DEFAULT 'standard';
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS season_folder BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tv_series ADD COLUMN IF NOT EXISTS tags          TEXT[] NOT NULL DEFAULT '{}';
