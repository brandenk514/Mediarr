-- 0007_indexers.down.sql
--
-- Reverts the M5 indexer definition store (#30). It is a standalone table with
-- no foreign keys, so a single drop removes the table and its index.

DROP TABLE IF EXISTS indexers;
