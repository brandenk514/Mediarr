-- 0019_music_l4_activity.down.sql
-- Revert the L4 Activity music display columns.
ALTER TABLE blocklist DROP COLUMN IF EXISTS album_title;
ALTER TABLE blocklist DROP COLUMN IF EXISTS quality;
ALTER TABLE blocklist DROP COLUMN IF EXISTS formats;

ALTER TABLE music_queue DROP COLUMN IF EXISTS album_title;
ALTER TABLE music_queue DROP COLUMN IF EXISTS quality;
ALTER TABLE music_queue DROP COLUMN IF EXISTS formats;
ALTER TABLE music_queue DROP COLUMN IF EXISTS track_title;
