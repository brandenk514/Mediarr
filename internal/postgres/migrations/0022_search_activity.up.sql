-- Manual search activity history (Prowlarr PR3): the query-history table and
-- the grab store behind the Search/Stats pages.
--
-- search_queries is one row per attempted indexer search (POST /api/v1/search
-- records one per indexer, including failures). indexer_id is nullable so a
-- failed build (no resolved id / unknown kind) still lands a row; indexer_name
-- is the display name. categories is the JSONB array the query used ([] when
-- none). elapsed_ms is the per-indexer wall-clock duration. at is the attempt
-- time. Retention/expiry is PR6's "Clean Up History" task — for now the table
-- is trivially clearable via DELETE (no scheduled job yet).
--
-- search_grabs records one row per successful grab (POST /api/v1/search/grab).
-- It is not listed in the History table; it exists to feed the stats totals
-- and the grabs time series.
CREATE TABLE IF NOT EXISTS search_queries (
    id            BIGSERIAL PRIMARY KEY,
    indexer_id    BIGINT,
    indexer_name  TEXT NOT NULL,
    term          TEXT NOT NULL,
    categories    JSONB NOT NULL DEFAULT '[]',
    ok            BOOLEAN NOT NULL DEFAULT false,
    elapsed_ms    BIGINT NOT NULL DEFAULT 0,
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_search_queries_at ON search_queries (at DESC);
CREATE INDEX IF NOT EXISTS idx_search_queries_indexer_name ON search_queries (indexer_name);

CREATE TABLE IF NOT EXISTS search_grabs (
    id            BIGSERIAL PRIMARY KEY,
    indexer_name  TEXT NOT NULL,
    title         TEXT NOT NULL,
    protocol      TEXT NOT NULL,
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_search_grabs_at ON search_grabs (at DESC);
CREATE INDEX IF NOT EXISTS idx_search_grabs_indexer_name ON search_grabs (indexer_name);
