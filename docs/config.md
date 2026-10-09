# Mediarr configuration

Mediarr is configured entirely through **environment variables**. There is no
file-based config; the single source of truth is the `config` package
(`internal/config/config.go`), which parses, validates, and applies defaults for
every runtime setting. No other package may read `os.Getenv` directly —
everything funnels through `config.Load`, so defaults, validation, and
redaction are enforced in one place.

The process entrypoint calls `config.LoadFromOS()`, which snapshots the current
environment and hands it to `Load`. **Invalid or missing required values fail
fast at startup** (the process refuses to boot) rather than failing later in the
pipeline.

A ready-to-edit template lives in [`.env.example`](../.env.example) at the repo
root. Copy it to `.env` and fill in the real values — `.env` is gitignored, so
real secrets never enter version control.

```bash
cp .env.example .env
# then edit .env
```

---

## 1. Required variables

These three have no safe default. The process will not start without them.

| Variable | Default | Format / validation | Purpose |
| --- | --- | --- | --- |
| `MEDIARR_POSTGRES_DSN` | — | `postgres://` or `postgresql://` scheme, must include a host | PostgreSQL connection string. |
| `MEDIARR_REDIS_URL` | — | `redis://` or `rediss://` scheme, must include a host | Redis connection URL. |
| `MEDIARR_ENCRYPTION_KEY` | — | Exactly **64 hex characters** (32 bytes) | Key for AES-256-GCM at-rest encryption of indexer / download-client API keys. |

### Postgres DSN

Accepted schemes are `postgres://` and `postgresql://`. A host is required.

```
MEDIARR_POSTGRES_DSN=postgres://mediarr:***@localhost:5432/mediarr?sslmode=disable
```

For local development `sslmode=disable` is fine. For TLS deployments use
`sslmode=require` (or a stronger setting) in the query string.

> **Fallback:** if `MEDIARR_POSTGRES_DSN` is unset, the loader also accepts
> `DATABASE_URL` (a common convention for managed-Postgres platforms). Set
> `MEDIARR_POSTGRES_DSN` to make the source explicit.

### Redis URL

Accepted schemes are `redis://` (plaintext) and `rediss://` (TLS). A host is
required.

```
MEDIARR_REDIS_URL=redis://localhost:6379
```

### Encryption key

Exactly 64 hexadecimal characters (case-insensitive), representing a 32-byte
key. Used to AES-256-GCM-encrypt secrets (indexer and download-client API keys)
at rest.

Generate one with the Makefile target or OpenSSL:

```bash
make key                 # → prints a 64-char hex key
# or
openssl rand -hex 32
```

---

## 2. HTTP / server variables

All optional; sensible defaults apply.

| Variable | Default | Description |
| --- | --- | --- |
| `MEDIARR_HTTP_ADDR` | `:8080` | Listen address for the HTTP server. |
| `MEDIARR_HTTP_BASE_URL` | `http://localhost:8080` | Public base URL used for CORS and generated links. Set this to the externally reachable URL (e.g. behind a reverse proxy / TLS terminator). |
| `MEDIARR_READ_TIMEOUT` | `15` | HTTP read timeout, **seconds**. |
| `MEDIARR_WRITE_TIMEOUT` | `60` | HTTP write timeout, **seconds**. |
| `MEDIARR_SHUTDOWN_TIMEOUT` | `10` | Seconds to drain in-flight requests on graceful shutdown. |

Numeric values that fail to parse (e.g. `MEDIARR_READ_TIMEOUT=abc`) fall back
to the default rather than aborting startup — the loader treats these as
soft settings.

---

## 3. Storage paths

| Variable | Default | Description |
| --- | --- | --- |
| `MEDIARR_MEDIA_ROOT` | `/media/movies` | Root directory where **imported** media files are placed. The import step moves files here. See [per-domain media roots](#per-domain-media-roots) for how each domain derives its own subtree. |
| `MEDIARR_DOWNLOADS_DIR` | `/downloads` | Directory where the download client writes completed files **before** import moves them under `MEDIARR_MEDIA_ROOT`. |

Both are mountable volumes in Docker deployments. The production compose file
and Helm chart (M7) bind-mount / claim these paths.

### Per-domain media roots

`MEDIARR_MEDIA_ROOT` is a single base path. Each domain derives its own
subtree from it so the four media kinds never collide on disk:

| Domain | On-disk root |
| --- | --- |
| Movies | `<MEDIARR_MEDIA_ROOT>` (used directly) |
| TV | `<MEDIARR_MEDIA_ROOT>/tv` |
| Music | `<MEDIARR_MEDIA_ROOT>/music` |
| Books | `<MEDIARR_MEDIA_ROOT>/books` |

If `MEDIARR_MEDIA_ROOT` is empty, the helpers fall back to the bare domain
name (`movies` / `tv` / `music` / `books`) so a relative layout still works.

Each domain then applies its own import layout under its root (e.g. books land
at `<root>/<Author>/<Title>/<Title>.<ext>`; see the per-domain
[`domains/<kind>`](../internal/domains) import code).

---

## 4. Security

| Variable | Default | Description |
| --- | --- | --- |
| `MEDIARR_AUTH_PINNING` | `true` | Single-user v1 mode: the first user created is the only one that can change credentials. Set to `false` to allow additional credential holders (multi-user). |

At-rest encryption of secrets uses `MEDIARR_ENCRYPTION_KEY` (see
[Encryption key](#encryption-key)). Secrets are stored only in the environment
or, once the app is running, encrypted in Postgres — never in plaintext at
rest.

---

## 5. Quality / format profiles

Profiles are **not** environment variables. They are defined in code per domain
and selected per item (movie/series/album/book) through the API. Each domain
ships a set of built-in profiles; the **default** for a newly monitored item is
listed below.

- **Movies & TV** — video quality profiles (resolution + optional codec).
  Built-ins: `HD-1080p`, `Full-1080p`, `HD-Any`, `UHD-2160p`, `Any`.
  **Default: `HD-1080p`.**
  A release matches if it satisfies any one item; the highest matching item
  wins. "Better than requested" is accepted (a 2160p release satisfies a
  1080p item).

- **Music** — audio format + optional minimum bit-rate. Built-ins:
  `Lossless`, `Lossless-or-320`, `320K`, `Any`. **Default: `Lossless`.**
  `Lossless` accepts FLAC / APE / WAV (bit-exact); `320K` accepts 320 kbps
  MP3 / AAC / M4A.

- **Books** — e-book format (format preference, not a quality ladder).
  Built-ins: `Best` (epub ▸ azw3 ▸ mobi), `EPUB`, `Kindle` (azw3 ▸ mobi),
  `Any`. **Default: `Best`.**

Profile matching is pure (no I/O) and lives in
[`internal/domains/<kind>`](../internal/domains). To add or change a built-in
profile, edit the domain's `quality.go` / `format.go` — not a config file.

---

## 6. Quick reference (`.env.example`)

The canonical, commented template:

```bash
# ---- HTTP -----------------------------------------------------------------
MEDIARR_HTTP_ADDR=:8080
MEDIARR_HTTP_BASE_URL=http://localhost:8080
# MEDIARR_READ_TIMEOUT=15
# MEDIARR_WRITE_TIMEOUT=60
# MEDIARR_SHUTDOWN_TIMEOUT=10

# ---- Postgres -------------------------------------------------------------
# sslmode=disable is fine for local dev; use require for TLS deployments.
MEDIARR_POSTGRES_DSN=postgres://mediarr:***@localhost:5432/mediarr?sslmode=disable

# ---- Redis ----------------------------------------------------------------
MEDIARR_REDIS_URL=redis://localhost:6379
# MEDIARR_REDIS_DB=0

# ---- Security -------------------------------------------------------------
# 64 hex chars (32 bytes). Generate with: make key  (or: openssl rand -hex 32)
MEDIARR_ENCRYPTION_KEY=
# Single-user v1 mode: first user created is the only credential holder.
MEDIARR_AUTH_PINNING=true
```

`MEDIARR_REDIS_DB` (default `0`) selects the Redis logical database index.
