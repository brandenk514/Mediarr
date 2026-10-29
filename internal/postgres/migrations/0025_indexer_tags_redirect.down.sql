-- 0025_indexer_tags_redirect.down.sql
--
-- Reverts the PR4 indexer tags/redirect columns.
ALTER TABLE indexers
  DROP COLUMN IF EXISTS tags,
  DROP COLUMN IF EXISTS redirect;
