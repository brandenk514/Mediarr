-- 0006_books_pipeline.down.sql
--
-- Reverts the M4 books wanted/queue/history tables. Children (the indexes)
-- drop with their tables, so only the three tables need explicit drops, in
-- reverse dependency order (all reference the title/edition tables, not each
-- other).

DROP TABLE IF EXISTS book_history;
DROP TABLE IF EXISTS book_queue;
DROP TABLE IF EXISTS book_wanted;
