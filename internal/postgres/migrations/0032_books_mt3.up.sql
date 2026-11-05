-- 0032_books_mt3.up.sql
-- MT3 (metadata source: cache + provenance) — the resolved-work store.
--
-- Keyless Open Library has no SLA, so re-fetching a work on every lookup
-- is not viable. This table persists each resolved book work with its
-- source provenance and the fetch timestamp the TTL cache reads:
--
--   * work_key:  the resolved OpenLibrary work key (bare id, e.g.
--     "OL8189755W" — the provider's Result.ID shape).
--   * source / source_id: where the row came from. For now the only source
--     is "openlibrary" and source_id is the OL key itself; at the B6
--     drop-in migration, imported rows may carry a Readarr source id
--     instead, so (source, source_id) — not work_key — is the primary key.
--     work_key is indexed (not unique): two different sources can resolve
--     to the same OpenLibrary work.
--   * fetched_at: when the row was last fetched from its source — the TTL
--     anchor the lookup and the refresh command read.
CREATE TABLE IF NOT EXISTS book_work_metadata (
    source     TEXT NOT NULL,
    source_id  TEXT NOT NULL,
    work_key   TEXT NOT NULL,
    title      TEXT NOT NULL,
    author     TEXT NOT NULL DEFAULT '',
    year       INT NOT NULL DEFAULT 0,
    cover_url  TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source, source_id)
);
CREATE INDEX IF NOT EXISTS idx_book_work_metadata_work_key ON book_work_metadata (work_key);
