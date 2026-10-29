-- Revert the Lidarr L4 metadata columns.
ALTER TABLE music_artists DROP COLUMN IF EXISTS cover;
ALTER TABLE music_albums  DROP COLUMN IF EXISTS cover;
ALTER TABLE music_albums  DROP COLUMN IF EXISTS labels;
ALTER TABLE music_albums  DROP COLUMN IF EXISTS genres;
