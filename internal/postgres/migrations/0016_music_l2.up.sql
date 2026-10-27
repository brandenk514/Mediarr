-- Lidarr L2: add-artist flow (monitoring policy + tags).
-- Additive: existing rows keep "" (empty) = "all" / "all_albums" and no tags.
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS monitor_policy     TEXT NOT NULL DEFAULT '';
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS monitor_new_albums TEXT NOT NULL DEFAULT '';
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS tags               TEXT[] NOT NULL DEFAULT '{}';
