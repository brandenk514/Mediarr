-- 0020_indexer_hub.down.sql
--
-- Reverts the PR1 indexer-hub list columns.
ALTER TABLE indexers
  DROP COLUMN IF EXISTS privacy,
  DROP COLUMN IF EXISTS priority,
  DROP COLUMN IF EXISTS sync_profile,
  DROP COLUMN IF EXISTS added;
