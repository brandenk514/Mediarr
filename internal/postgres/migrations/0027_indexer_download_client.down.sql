-- 0027_indexer_download_client.down.sql
--
-- Reverts the PR5 per-indexer download-client binding column.

ALTER TABLE indexers
  DROP COLUMN IF EXISTS download_client_id;
