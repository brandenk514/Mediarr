-- 0020_indexer_hub.up.sql
--
-- Prowlarr PR1 (indexer-hub IA). Adds the four list columns the Indexers hub
-- table reads: Privacy, Priority, Sync Profile, and Added (created time).
--
-- Defaults match the wire defaults so existing rows render correctly in the
-- hub without a backfill: public / 25 / standard / created. `added` is
-- backfilled with the row's existing created_at when present, else now().
ALTER TABLE indexers
  ADD COLUMN IF NOT EXISTS privacy TEXT NOT NULL DEFAULT 'public',
  ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 25,
  ADD COLUMN IF NOT EXISTS sync_profile TEXT NOT NULL DEFAULT 'standard',
  ADD COLUMN IF NOT EXISTS added TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill `added` from the pre-existing created_at so the hub's "Added"
-- column reflects when the indexer was actually created, not when the column
-- was added.
UPDATE indexers
SET added = COALESCE(created_at, now())
WHERE created_at IS NOT NULL;
