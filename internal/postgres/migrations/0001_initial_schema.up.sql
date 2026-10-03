-- 0001_initial_schema.up.sql
-- Initial schema for mediarr. Single-user v1: users table is present with a
-- single admin row expected to be created at first boot (see first-boot flow).

CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_uuid()

-- users ---------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    email         TEXT        NOT NULL DEFAULT '',
    role          TEXT        NOT NULL DEFAULT 'admin'
                  CHECK (role IN ('admin', 'user')),
    enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    password_hash TEXT        NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- api_tokens: stored as salted fingerprint; raw token is never persisted ---
CREATE TABLE IF NOT EXISTS api_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    prefix      TEXT        NOT NULL,
    fingerprint TEXT        NOT NULL UNIQUE,
    salt        BYTEA       NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

-- settings: free-form key/value for runtime config (JSON values) ----------
CREATE TABLE IF NOT EXISTS settings (
    key         TEXT        PRIMARY KEY,
    value_json  JSONB       NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- schema_migrations: tracks applied migrations ----------------------------
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT        PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
