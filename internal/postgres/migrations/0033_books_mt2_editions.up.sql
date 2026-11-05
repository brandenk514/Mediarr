-- 0033_books_mt2_editions.up.sql
-- MT2 (automatic edition resolution) — cache the work's editions with the
-- resolved-work row (the MT3 provenance store).
--
-- The work's editions are fetched from Open Library WITH the work (the
-- work-details fetch) and cached in the same row, so:
--   * a fresh row serves its editions without an Open Library call;
--   * a work-key refresh refreshes the editions too (the same
--     TTL/refresh path — the column is part of the row the upsert re-anchors).
--
-- '[]' (an empty JSON array) for rows resolved without a details fetch
-- (a plain search resolution carries no editions) — the column is NOT NULL
-- so a NULL jsonb can never be written by mistake.
ALTER TABLE book_work_metadata
    ADD COLUMN IF NOT EXISTS editions JSONB NOT NULL DEFAULT '[]'::jsonb;
