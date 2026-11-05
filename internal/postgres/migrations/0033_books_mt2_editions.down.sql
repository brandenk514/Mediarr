-- 0033_books_mt2_editions.down.sql — inverse of 0033_books_mt2_editions.up.sql.
ALTER TABLE book_work_metadata DROP COLUMN IF EXISTS editions;
