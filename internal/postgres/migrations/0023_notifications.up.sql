-- 0023_notifications.up.sql
--
-- Prowlarr PR4 (Notifications section). A saved notification sink: webhook /
-- Plex / Jellyfin / Emby. kind is constrained to the four sink kinds
-- (internal/notify.SinkKind).
--
-- token is the AES-256-GCM ciphertext (hex) of the sink's token, encrypted at
-- rest with the app vault key — the same pattern as
-- indexers.api_key_encrypted. It is NULL when the sink has no token. The repo
-- layer (internal/postgres/notifications_repo.go) is the only place that
-- encrypts / decrypts; the column stores ciphertext only, so a leaked dump
-- does not expose a usable token.

CREATE TABLE IF NOT EXISTS notifications (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('webhook', 'plex', 'jellyfin', 'emby')),
    enabled     BOOLEAN NOT NULL DEFAULT true,
    url         TEXT NOT NULL DEFAULT '',
    section_id  TEXT NOT NULL DEFAULT '',
    token       TEXT,
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
