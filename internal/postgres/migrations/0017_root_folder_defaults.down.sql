-- Lidarr L3: per-root default profiles (revert).
ALTER TABLE root_folders DROP COLUMN IF EXISTS default_metadata_profile;
ALTER TABLE root_folders DROP COLUMN IF EXISTS default_quality_profile;
