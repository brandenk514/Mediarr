-- Indexer definition library (Prowlarr PR2): the built-in Prowlarr indexer
-- definition set, seeded from the embedded seed
-- (internal/indexers/definitions/definitions.json) at boot when empty.
--
-- seed_revision is the identity of the embedded seed that produced the row,
-- so the definition-update task (Prowlarr PR6) can compare it against the
-- current seed to detect stale rows and re-upsert.
CREATE TABLE IF NOT EXISTS indexer_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    privacy TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    categories JSONB NOT NULL DEFAULT '[]',
    urls JSONB NOT NULL DEFAULT '[]',
    legacy_urls JSONB NOT NULL DEFAULT '[]',
    fields JSONB NOT NULL DEFAULT '[]',
    raw TEXT NOT NULL DEFAULT '',
    seed_revision TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_indexer_definitions_protocol ON indexer_definitions (protocol);
CREATE INDEX IF NOT EXISTS idx_indexer_definitions_privacy ON indexer_definitions (privacy);
CREATE INDEX IF NOT EXISTS idx_indexer_definitions_name ON indexer_definitions (name);

-- Per-indexer config-contract values (Prowlarr PR2). Password fields are
-- stored encrypted inside this JSON; the repo layer is the only layer that
-- touches ciphertext (mirrors the indexers.api_key convention). Empty '{}'
-- means a legacy indexer added without a definition. definition_name names
-- the library definition the indexer was added from (NULL = legacy).
ALTER TABLE indexers
    ADD COLUMN IF NOT EXISTS config_fields_json TEXT NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS definition_name TEXT;
