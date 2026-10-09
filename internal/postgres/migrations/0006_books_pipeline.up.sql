-- 0006_books_pipeline.up.sql
--
-- M4 (card #27) books wanted/queue/history tables. Mirrors the M3 music
-- pipeline (0004_music) so the books pipeline reuses the same search →
-- wanted → download → import → history flow.
--
-- Granularity mirrors music: a wanted row is at title or edition granularity
-- (edition_id = 0 is the whole title, edition_id > 0 is a specific edition),
-- exactly as music_wanted uses (album_id, track_id) with track_id = 0 for a
-- whole album. Queue and history rows are grouped by author (mirroring
-- music's artist grouping) so the pipeline can service one author's library
-- in a single run.

-- book_wanted: a "we want a file" request at title or edition granularity.
-- edition_id = 0 is the whole-title request; edition_id > 0 is a specific
-- edition. The composite key (title_id, edition_id) makes EnsureWanted
-- idempotent (ON CONFLICT DO NOTHING), mirroring music_wanted.
CREATE TABLE IF NOT EXISTS book_wanted (
    title_id      BIGINT      NOT NULL REFERENCES book_titles(id) ON DELETE CASCADE,
    edition_id    BIGINT      NOT NULL DEFAULT 0,  -- 0 = whole title
    status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'satisfied')),
    release_title TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    satisfied_at  TIMESTAMPTZ,
    PRIMARY KEY (title_id, edition_id)
);
CREATE INDEX IF NOT EXISTS idx_book_wanted_pending ON book_wanted(status);

-- book_queue: one download job for a title release (or a single edition).
CREATE TABLE IF NOT EXISTS book_queue (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id       BIGINT      NOT NULL REFERENCES book_authors(id) ON DELETE CASCADE,
    title_id        BIGINT      NOT NULL REFERENCES book_titles(id) ON DELETE CASCADE,
    release_title   TEXT        NOT NULL,
    indexer         TEXT        NOT NULL DEFAULT '',
    download_client TEXT        NOT NULL DEFAULT '',
    state           TEXT        NOT NULL DEFAULT 'queued'
                    CHECK (state IN ('queued', 'downloading', 'complete', 'failed')),
    progress        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_book_queue_author ON book_queue(author_id);
CREATE INDEX IF NOT EXISTS idx_book_queue_state ON book_queue(state);

-- book_history: append-only audit trail of an author's library lifecycle.
CREATE TABLE IF NOT EXISTS book_history (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id BIGINT      NOT NULL REFERENCES book_authors(id) ON DELETE CASCADE,
    event     TEXT        NOT NULL,
    detail    TEXT        NOT NULL DEFAULT '',
    at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_book_history_author ON book_history(author_id, at DESC);
