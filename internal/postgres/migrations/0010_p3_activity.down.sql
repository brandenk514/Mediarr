-- Reverse of 0010_p3_activity.up.sql.
ALTER TABLE tv_wanted DROP COLUMN IF EXISTS quality;
ALTER TABLE tv_episodes DROP COLUMN IF EXISTS air_date;
DROP INDEX IF EXISTS blocklist_parent_idx;
DROP INDEX IF EXISTS blocklist_kind_idx;
DROP TABLE IF EXISTS blocklist;
