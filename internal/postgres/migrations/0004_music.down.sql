-- 0004_music.down.sql
-- Reverts the M3 music domain tables. Drop the child tables (the ones
-- referencing music_albums / music_artists) before their parents so the
-- foreign keys resolve cleanly.
DROP TABLE IF EXISTS music_history;
DROP TABLE IF EXISTS music_queue;
DROP TABLE IF EXISTS music_wanted;
DROP TABLE IF EXISTS music_tracks;
DROP TABLE IF EXISTS music_albums;
DROP TABLE IF EXISTS music_artists;
