-- 0031_readarr_b6.up.sql
-- Readarr B6 (drop-in migration) — the resumable migration state.
--
-- A drop-in import can be long (hundreds of books, each an Open Library
-- resolution + a few writes). If the run is interrupted (a source/target
-- failure mid-way, or a host restart), a re-run must pick up where it left
-- off rather than re-importing. The Sonarr/Radarr migrators achieve resume
-- by "re-run converges idempotently"; the Readarr migrator goes one step
-- further and records the migrated BOOK KEYS (the OpenLibrary work key, or
-- "local:<id>" for an unresolved book) so a resume skips them. The state is
-- Postgres (not in-memory) so it survives a process restart.
--
-- Keyed by (source_url, book_key): a run is per-source-instance, and the
-- book_key is the migration's stable identity for a source book.

CREATE TABLE IF NOT EXISTS readarr_migration_state (
    source_url   TEXT NOT NULL,
    book_key     TEXT NOT NULL,
    migrated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_url, book_key)
);
