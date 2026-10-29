-- 0019_music_l4_activity.up.sql
-- Lidarr L4 (Activity/Wanted/Calendar music wiring): the music-specific
-- display columns on the shared queue + blocklist row shapes (Lidarr
-- queue/blocklist: Artist / Album Title / Quality / Formats / Track Title /
-- Date).
--
-- music_queue gains the four display fields that are not derivable from the
-- existing columns (the artist is the queue parent, already selected by the
-- join; release_title carries the scene name). blocklist gains the three
-- display fields that are not derivable from the release title (the artist
-- name is ParentName, resolved at read time from the parent join).
-- Additive: existing rows keep '' (unknown).

ALTER TABLE music_queue ADD COLUMN IF NOT EXISTS album_title TEXT NOT NULL DEFAULT '';
ALTER TABLE music_queue ADD COLUMN IF NOT EXISTS quality     TEXT NOT NULL DEFAULT '';
ALTER TABLE music_queue ADD COLUMN IF NOT EXISTS formats     TEXT NOT NULL DEFAULT '';
ALTER TABLE music_queue ADD COLUMN IF NOT EXISTS track_title TEXT NOT NULL DEFAULT '';

ALTER TABLE blocklist ADD COLUMN IF NOT EXISTS album_title TEXT NOT NULL DEFAULT '';
ALTER TABLE blocklist ADD COLUMN IF NOT EXISTS quality     TEXT NOT NULL DEFAULT '';
ALTER TABLE blocklist ADD COLUMN IF NOT EXISTS formats     TEXT NOT NULL DEFAULT '';
