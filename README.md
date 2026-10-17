# Mediarr

A single-binary media manager for **movies, TV, music, and books** — one
replacement for the ARR stack (Sonarr + Radarr + Lidarr + Readarr). A Go
backend owns the whole pipeline (indexer search → download client → import), a
React/TypeScript SPA drives it, Postgres is the source of truth, and Redis
handles caching and live event push.

## Features

- **Four libraries.** Movies (title + year), TV (series → seasons →
  episodes, single episodes or season packs), music (artist → album → tracks),
  and books (author → title → editions). Each supports monitoring, per-item
  quality/format profiles, and a full add → want → search → download → import
  pipeline.
- **Indexer support.** Torznab, torrent-RSS, and NZB adapters, plus a
  development `fake` indexer. Concurrent fan-out search across all enabled
  indexers with per-indexer timeouts, 10-minute result caching, in-process
  rate limiting, and health tracking with on-demand tests. Indexer API keys
  are AES-256-GCM-encrypted at rest. Manage indexers (add/edit/enable/test)
  from the UI.
- **Download pipeline.** qBittorrent (Web API) and SABnzbd (JSON API)
  adapters, plus a built-in `mock` client so the full pipeline runs with no
  external software. Pipeline runs are operator-driven (`POST …/pipeline`):
  search → profile match → queue → client send → poll → import under the
  domain's media root. Live queue and history updates stream over a
  WebSocket.
- **Dashboard.** Cross-library counts, queue depth by state, and recent
  activity in one authenticated endpoint that the UI refreshes.
- **Settings.** Download-client kind + connection details (credentials
  encrypted at rest) and per-domain library overrides (media roots,
  quality/format profiles) — all at runtime via the API, persisted in
  Postgres.
- **Built-in profiles.** Movies: `HD-1080p` (default), `Full-1080p`,
  `HD-Any`, `UHD-2160p`, `Any`. Music: `Lossless` (default),
  `Lossless-or-320`, `320K`, `Any`. Books: `Best` (default), `EPUB`,
  `Kindle`, `Any`.
- **Single-user auth.** First login mints the admin (username + ≥10-char
  password); every request after that uses a Bearer token. Argon2id password
  hashing; tokens are opaque `ma_`-prefixed secrets stored only as salted
  SHA-256 fingerprints and revocable.
- **Observability.** Auth-gated Prometheus `/metrics`, `/healthz`, `/readyz`,
  `/version`, structured JSON logs. Ship-included Prometheus + Grafana compose
  stack (`deploy/prometheus`, `deploy/grafana`,
  `deploy/compose.observability.yml`).
- **Deployment.** Multi-arch distroless Docker image (nonroot, read-only
  rootfs, healthcheck subcommand), dev compose stack, and a Helm chart with a
  dev-parity security model (see `deploy/helm/README.md`).

## Quick start

Requirements: Go 1.27+, Docker (for `make dev`), Node 22+ (for the UI).

1. **Generate an encryption key** (the process cannot boot without it):

   ```sh
   make key        # or: openssl rand -hex 32
   ```

2. **Start the backend + Postgres + Redis:**

   ```sh
   export MEDIARR_ENCRYPTION_KEY=<the 64-hex key>
   make dev        # docker compose -f deploy/compose.dev.yml up --build
   ```

   The backend listens on `:8080`. `GET /` returning 404 is normal — the API
   lives under `/api/v1/...`.

3. **First boot: create the admin.** While the `users` table is empty, the
   first `POST /api/v1/auth/login` call creates the admin user (3–32 char
   username of `[A-Za-z0-9_-]`, password ≥ 10 chars) and returns the API
   token. Log in through the UI to get the token normally.

4. **Run the UI (separate process, dev only):**

   ```sh
   cd ui && npm ci && npm run dev   # Vite on :5173, proxies /api → :8080
   ```

The only required env vars are `MEDIARR_ENCRYPTION_KEY` (64 hex chars),
`MEDIARR_POSTGRES_DSN`, and `MEDIARR_REDIS_URL`; everything else has a
default. See `.env.example` and `docs/config.md` for the full surface.

## Repository layout

```
cmd/mediarr/           the single Go binary (main + healthcheck subcommand)
internal/              backend packages: api, auth, config, dashboard,
                       domains/{movies,tv,music,books}, downloads, health,
                       indexers, metrics, paging, postgres (+migrations/),
                       providers, queue, redis, secrets,
                       services/{movies,tv,music,books}, settings, store
ui/                    Vite + React + TypeScript SPA (vitest)
deploy/                Dockerfile, compose.dev.yml,
                       compose.observability.yml, prometheus/, grafana/,
                       helm/ (chart + README + test.sh)
docs/                  PLAN.md, architecture.md, config.md,
                       observability.md, loadtest.md
test/                  e2e/ (pipeline E2E), loadtest/ (10k-item harness)
scripts/               release-notes.sh
.github/workflows/     ci.yml (the CI gate, incl. govulncheck/Semgrep/Trivy)
```

## Makefile targets

| Target | What it does |
|---|---|
| `make help` | Show available targets |
| `make build` | Compile the service binary into `./bin` |
| `make run` | Run locally (needs env from `.env` or environment) |
| `make test` | Unit + integration tests (integration needs Docker) |
| `make test-unit` | Unit tests only (no Docker) |
| `make cover` | Test with coverage report |
| `make loadtest` | Load test: 10k items, latency, sustained import (Docker) |
| `make lint` | `go vet` + `gofmt` check |
| `make tidy` | Sync `go.mod` / `go.sum` |
| `make scan` | Security scans: govulncheck (+ gosec if installed) |
| `make image` | Build the Docker image (test artifact) |
| `make image-push` | Build and push to ghcr (requires GHCR login) |
| `make dev` | Start the full dev stack (app + postgres + redis) via compose |
| `make dev-down` | Stop the dev stack |
| `make key` | Generate a new encryption key (32 bytes hex) |
| `make clean` | Remove local build artifacts |

## API overview

Base path `/api/v1`, Bearer-token auth (one admin user; the first login mints
it). Public: `GET /healthz`, `GET /readyz`, `GET /version`,
`POST /api/v1/auth/login`. Auth-gated: `auth/me`; `dashboard`; `queue`
(+ per-kind history) and the live `WS /api/v1/events`; per-domain CRUD +
`POST …/pipeline` for movies, TV (series/seasons/episodes), music
(artist/album/track), and books (author/title/edition); indexer CRUD +
test/stats; `search/*` (movies, tv, tv-tmdb, artists, books/authors,
books/works) and `profiles`; settings (download-client, library overrides);
and `GET /metrics`. Full route list: `docs/architecture.md` §4.

## Documentation

| Doc | Contents |
|---|---|
| `docs/PLAN.md` | Original design intent, milestones M0–M7 |
| `docs/architecture.md` | As-built architecture: components, module map, routes, auth, pipeline, data model, Redis usage, config & secrets, observability, deployment, testing |
| `docs/config.md` | Full environment-variable reference + defaults |
| `docs/observability.md` | Metrics reference and Prometheus/Grafana wiring |
| `docs/loadtest.md` | Load-test methodology, thresholds, baseline |
| `RELEASE.md` | Release process (tags, cosign signing, deployment) |
| `deploy/helm/README.md` | Helm chart values and security model |

## Testing & CI

- `make test` — unit + testcontainers integration (real Postgres 16 + Redis 7);
  CI enforces a 60% overall coverage floor.
- `go test ./test/e2e/...` — pipeline E2E with the real qBittorrent/SABnzbd
  adapters against in-process HTTP stubs.
- `make loadtest` — 10k-item latency/import/RSS harness.
- `cd ui && npm test` — vitest (plus `npm run lint` / `npm run build`).
- CI (`.github/workflows/ci.yml`) additionally gates on: `govulncheck`,
  **Semgrep** (`p/golang` + `p/typescript`, pinned `semgrep==1.181.0`),
  **Trivy** image scan, SBOM, **cosign** keyless signing of published images,
  a compose smoke test, and release-notes generation on tags.
