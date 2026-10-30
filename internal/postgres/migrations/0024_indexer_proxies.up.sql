-- 0024_indexer_proxies.up.sql
--
-- Prowlarr PR4 (Indexer Proxies). A FlareSolverr instance that indexers whose
-- tags intersect this row's tags are routed through.
--
-- password is the AES-256-GCM ciphertext (hex) of the instance's basic-auth
-- password, encrypted at rest with the app vault key — the same pattern as
-- indexers.api_key_encrypted. It is NULL when the instance has no password.
-- The repo layer (internal/postgres/indexer_proxies_repo.go) is the only
-- place that encrypts / decrypts; the column stores ciphertext only.
--
-- tags is a JSONB array of the tag labels that route indexers to this proxy.

CREATE TABLE IF NOT EXISTS indexer_proxies (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    username    TEXT NOT NULL DEFAULT '',
    password    TEXT,
    tags        JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
