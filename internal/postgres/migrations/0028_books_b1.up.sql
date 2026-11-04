-- 0028_books_b1.up.sql
-- Readarr B1 (Books library parity) — the enriched edition model, book files,
-- and series:
--
--   * book_editions gains `language`: the edition is now the (format,
--     language, monitored) triple, Books' analogue of Radarr's edition model.
--   * book_files: an imported e-book file and its media info (pages, size,
--     format, language, added date). A file belongs to a title and optionally
--     to the edition it matches (edition_id = 0 when no edition matches).
--     (title_id, path) is the file's identity: a re-import of the same file
--     must not duplicate the row.
--   * book_series + book_series_members: books <-> series links (Readarr's
--     "Series", the basis of B4's bookshelves). A series is named (unique);
--     a book can belong to many series and a series can contain many books.
--
-- Migration only: data entry stays manual in B1 (the MT2 provider-fed
-- editions subtask and B3's unmapped-file import are out of scope).

ALTER TABLE book_editions ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS book_files (
    id          BIGSERIAL PRIMARY KEY,
    title_id    BIGINT NOT NULL REFERENCES book_titles (id) ON DELETE CASCADE,
    -- 0 = not tied to a specific edition (no edition matched the imported file).
    edition_id  BIGINT NOT NULL DEFAULT 0,
    path        TEXT NOT NULL,
    format      TEXT NOT NULL,
    size        BIGINT NOT NULL DEFAULT 0,
    pages       INT NOT NULL DEFAULT 0,
    language    TEXT NOT NULL DEFAULT '',
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (title_id, path)
);
CREATE INDEX IF NOT EXISTS idx_book_files_title ON book_files (title_id);
CREATE INDEX IF NOT EXISTS idx_book_files_edition ON book_files (edition_id);

CREATE TABLE IF NOT EXISTS book_series (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS book_series_members (
    series_id   BIGINT NOT NULL REFERENCES book_series (id) ON DELETE CASCADE,
    title_id    BIGINT NOT NULL REFERENCES book_titles (id) ON DELETE CASCADE,
    PRIMARY KEY (series_id, title_id)
);
CREATE INDEX IF NOT EXISTS idx_book_series_members_title ON book_series_members (title_id);
