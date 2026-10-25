-- Root-folder registry (P2): on-disk media roots consumed by Add-New and
-- Library Import.
CREATE TABLE IF NOT EXISTS root_folders (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    path       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
