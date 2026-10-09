-- 0005_books.up.sql
-- M4 (Books) — data model (issue #24): the author / title / edition hierarchy,
-- mirroring the music artist / album / track model from 0004_music.
--
-- A book is identified at the TITLE level (author + title); an EDITION is a
-- specific publication of that title (a format + ISBN/publisher/year), which
-- is what gets matched, downloaded, and imported. Monitoring is carried on
-- each level so a whole author, a whole title, or a single edition can be
-- tracked. The edition is the unit of the format matcher (#25) and of the
-- wanted/monitor granularity (#27) — the edition's natural key is (title, ISBN),
-- falling back to (title, format, publisher, year) when no ISBN is known.
--
-- NOTE: wanted / queue / history tables for books are deliberately NOT created
-- here; they are introduced together with the wanted/monitor work (#27) and the
-- pipeline (#28), matching how this migration stays focused on the data model.

CREATE TABLE IF NOT EXISTS book_authors (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    monitored   BOOLEAN NOT NULL DEFAULT true,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS book_titles (
    id          BIGSERIAL PRIMARY KEY,
    author_id   BIGINT NOT NULL REFERENCES book_authors (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    monitored   BOOLEAN NOT NULL DEFAULT false,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (author_id, name)
);
CREATE INDEX IF NOT EXISTS idx_book_titles_author ON book_titles (author_id);

CREATE TABLE IF NOT EXISTS book_editions (
    id          BIGSERIAL PRIMARY KEY,
    title_id    BIGINT NOT NULL REFERENCES book_titles (id) ON DELETE CASCADE,
    -- epub / mobi / azw3 (PLAN §15.4). Free text here (validated in the domain
    -- format matcher, #25) so the schema is not coupled to the format list.
    format      TEXT NOT NULL,
    publisher   TEXT,
    isbn        TEXT,
    year        INT,
    pages       INT,
    monitored   BOOLEAN NOT NULL DEFAULT false,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- An edition is a specific publication: the same ISBN under one title must
    -- not be added twice. NULLs are distinct in Postgres UNIQUE, so editions
    -- without an ISBN are not collapsed on this constraint (the no-ISBN fallback
    -- key is enforced in the domain).
    UNIQUE (title_id, isbn)
);
CREATE INDEX IF NOT EXISTS idx_book_editions_title ON book_editions (title_id);
