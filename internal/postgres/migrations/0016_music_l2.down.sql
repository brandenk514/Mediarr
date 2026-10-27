-- Revert the Lidarr L2 add-artist fields.
ALTER TABLE music_artists DROP COLUMN IF EXISTS monitor_policy;
ALTER TABLE music_artists DROP COLUMN IF EXISTS monitor_new_albums;
ALTER TABLE music_artists DROP COLUMN IF EXISTS tags;
