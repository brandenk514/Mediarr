-- 0005_books.down.sql — inverse of 0005_books.up.sql.
-- Drop children before parents so the ON DELETE CASCADE references can't block.
DROP TABLE IF EXISTS book_editions;
DROP TABLE IF EXISTS book_titles;
DROP TABLE IF EXISTS book_authors;
