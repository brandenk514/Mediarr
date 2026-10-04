-- 0003_tv.down.sql
-- Reverts the M2 TV domain tables. Drop the child tables (the ones referencing
-- tv_series) before tv_series itself so the foreign keys resolve cleanly.
DROP TABLE IF EXISTS tv_history;
DROP TABLE IF EXISTS tv_queue;
DROP TABLE IF EXISTS tv_wanted;
DROP TABLE IF EXISTS tv_episodes;
DROP TABLE IF EXISTS tv_series;
