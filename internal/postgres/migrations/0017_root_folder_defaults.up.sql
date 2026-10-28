-- Lidarr L3: per-root default profiles.
-- Additive: existing rows keep '' = unset (the app-level default applies).
ALTER TABLE root_folders ADD COLUMN IF NOT EXISTS default_quality_profile  TEXT NOT NULL DEFAULT '';
ALTER TABLE root_folders ADD COLUMN IF NOT EXISTS default_metadata_profile TEXT NOT NULL DEFAULT '';
