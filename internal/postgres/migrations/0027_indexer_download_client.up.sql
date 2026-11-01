-- 0027_indexer_download_client.up.sql
--
-- Prowlarr PR5 (per-indexer download-client binding). An indexer may name the
-- download client its grabs route to; NULL keeps the default routing (the
-- first enabled client whose kind handles the release protocol).

ALTER TABLE indexers
  ADD COLUMN IF NOT EXISTS download_client_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_indexers_download_client_id
  ON indexers (download_client_id);
