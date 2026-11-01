-- 0026_download_clients.up.sql
--
-- Prowlarr PR5 (download clients). Replaces the legacy single
-- settings/download-client row with a proper client LIST: one row per saved
-- download client (qBittorrent / SABnzbd / Transmission), in `position`
-- order. The api_key column holds the vault-ENCRYPTED key (see
-- internal/postgres/notifications_repo.go for the at-rest pattern) — never
-- plaintext, unlike the legacy single config.
--
-- The legacy config is seeded into this table at boot (cmd/mediarr/main.go,
-- seedLegacyDownloadClient) because raw SQL cannot call the vault.

CREATE TABLE IF NOT EXISTS download_clients (
  id           BIGSERIAL PRIMARY KEY,
  name         TEXT NOT NULL,
  kind         TEXT NOT NULL
    CHECK (kind IN ('qbittorrent', 'sabnzbd', 'transmission')),
  enabled      BOOLEAN NOT NULL DEFAULT true,
  position     INTEGER NOT NULL DEFAULT 1,
  base_url     TEXT NOT NULL DEFAULT '',
  username     TEXT NOT NULL DEFAULT '',
  api_key      TEXT NOT NULL DEFAULT '',
  complete_dir TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_download_clients_position
  ON download_clients (position, id);
