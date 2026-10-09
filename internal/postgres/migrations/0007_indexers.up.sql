-- 0007_indexers.up.sql
--
-- M5 (issue #30) — the shared indexer definition store (PLAN §4, §7). This is
-- the Prowlarr-role "indexer definitions as code + user config": one row per
-- configured indexer, shared by all four media types.
--
-- `kind` names the protocol adapter the provider framework instantiates
-- (torrent-rss / torznab / nzb / fake). The framework's factory (#30) maps
-- kind -> adapter; the real protocol adapters land in #31-33. The fake kind
-- keeps the M1/M2 reference implementation usable from configuration.
--
-- `api_key_encrypted` is the AES-256-GCM ciphertext (hex) of the indexer's
-- API key, encrypted at rest with the app vault key (PLAN §7: "encrypted at
-- rest ... key from env var — never logged, never in the image"). It is NULL
-- for keyless indexers (e.g. public torrent-RSS feeds). The repo layer
-- (internal/postgres/indexer_repo.go) is the only place that encrypts /
-- decrypts; the column stores ciphertext only, so a leaked dump does not
-- expose usable keys.
--
-- `settings_json` holds protocol-specific configuration (columns, categories,
-- RSS URL, NZB source, ...) as an opaque JSON blob. It is free-form so adding
-- an adapter in #31-33 does not require a schema migration per option.
--
-- `last_test` is the timestamp of the most recent health probe. It is NULL
-- until the indexer has been tested (health tracking + stats is #34).

CREATE TABLE IF NOT EXISTS indexers (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    kind               TEXT NOT NULL,
    base_url           TEXT NOT NULL DEFAULT '',
    api_key_encrypted  TEXT,
    settings_json      TEXT NOT NULL DEFAULT '{}',
    enabled            BOOLEAN NOT NULL DEFAULT true,
    last_test          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_indexers_enabled ON indexers (enabled);
