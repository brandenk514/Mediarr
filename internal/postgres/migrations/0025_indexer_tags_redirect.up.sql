-- 0025_indexer_tags_redirect.up.sql
--
-- Prowlarr PR4 (indexer model). Adds the two fields that land on the indexer
-- this phase: tags (the tag labels assigned to the indexer; its traffic is
-- routed through any proxy carrying a matching tag) and redirect (a
-- redirected indexer records grabs for stats parity but does not send them to
-- the download client).
--
-- Both are additive, non-destructive columns with wire defaults so existing
-- rows read correctly without a backfill (tags [] and redirect false).

ALTER TABLE indexers
  ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS redirect BOOLEAN NOT NULL DEFAULT false;
