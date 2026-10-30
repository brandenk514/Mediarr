-- 0024_indexer_proxies.down.sql
--
-- Reverts the PR4 indexer_proxies table. It is a standalone table with no
-- foreign keys, so a single drop removes it.
DROP TABLE IF EXISTS indexer_proxies;
