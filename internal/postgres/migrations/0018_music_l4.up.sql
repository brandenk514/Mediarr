-- Lidarr L4: music metadata + library import.
-- Additive: existing rows keep "" / 0 / empty arrays (unknown / none).
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS cover  TEXT     NOT NULL DEFAULT '';
ALTER TABLE music_albums  ADD COLUMN IF NOT EXISTS cover  TEXT     NOT NULL DEFAULT '';
ALTER TABLE music_albums  ADD COLUMN IF NOT EXISTS labels TEXT[]   NOT NULL DEFAULT '{}';
ALTER TABLE music_albums  ADD COLUMN IF NOT EXISTS genres TEXT[]   NOT NULL DEFAULT '{}';
