-- 0031_readarr_b6.down.sql
-- Reverts the Readarr B6 resumable migration state table.
DROP TABLE IF EXISTS readarr_migration_state;
