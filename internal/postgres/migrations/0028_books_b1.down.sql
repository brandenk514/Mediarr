-- 0028_books_b1.down.sql — inverse of 0028_books_b1.up.sql.
DROP TABLE IF EXISTS book_series_members;
DROP TABLE IF EXISTS book_series;
DROP TABLE IF EXISTS book_files;
ALTER TABLE book_editions DROP COLUMN IF EXISTS language;
