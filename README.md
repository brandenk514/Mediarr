# Mediarr

A single-binary media manager for **movies, TV, music, and books** — one
replacement for the ARR stack (Sonarr + Radarr + Lidarr + Readarr). A Go
backend owns the whole pipeline (indexer search → download client → import), a
React/TypeScript SPA drives it, Postgres is the source of truth, and Redis
handles caching and live event push.

> **A note on AI assistance.** This project was built with AI coding agents —
> most of the code was written by AI under human review. It's a disclosure,
> not a caveat: AI-written and human-written code go through the same gates,
> and AI-assisted contributions are welcome (see [CONTRIBUTING.md](CONTRIBUTING.md)).

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
- **Unified app shell (v0.4.0).** One eight-section sidebar — Dashboard,
  Discover, Library, Calendar, Activity, Indexers, Settings, System — with
  kind (TV / Movies / Music / Books) a facet under Library, not a top-level
  switch. The default landing is the unified cross-kind **Discover** page;
  **Library** is the cross-kind grid with a URL-synced kind filter
  (`/library?kind=…`), while the per-kind list and deep pages (e.g.
  `/movies/add`, `/tv/:id`) stay fully routed. The top-bar **global search**
  is cross-kind and deep-linkable (`/search/all?q=…`), distinct from the
  Indexers manual release-search page (`/search`).
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
- **Deployment.** Multi-arch distroless Docker image (nonroot uid 65532,
  read-only rootfs, healthcheck subcommand) that ships the built UI and serves
  it same-origin on `:8080` — one compose stack is the whole app (API +
  browser, no separate frontend service). Helm chart with a dev-parity
  security model (see `deploy/helm/README.md`).

## Quick start

> **⚠️ Do not expose the API port to the internet.** Mediarr is a
> self-hosted service for a trusted network (home lab / LAN / VPN). It has no
> internet-grade protection: the first login mints the admin user, tokens are
> the only auth, and there is no TLS termination. Keep `:8080` bound to your
> LAN or put it behind an authenticated reverse proxy; for remote access use
> a VPN or a tunnel with access controls (Tailscale, Cloudflare Tunnel),
> not a port forward.

**Prerequisites: a Docker host (with the compose plugin) and a web
browser — nothing else.** The deployment path needs no Go and no Node: the
UI is built into the image and served by the backend on `:8080`.

1. **Generate an encryption key** (the process cannot boot without it):

   ```sh
   export MEDIARR_ENCRYPTION_KEY="$(openssl rand -hex 32)"
   ```

   No output — the 64-hex key lives in the shell variable. (`make key`
   prints one, if you prefer the Makefile target.)

2. **Start the stack** — app + Postgres + Redis; this one stack is the
   whole app:

   ```sh
   docker compose -f deploy/compose.dev.yml up -d --build
   ```

   Only the backend's `:8080` is published. To stop later:
   `docker compose -f deploy/compose.dev.yml down -v` (the `-v` also removes
   the data volumes — that is how you reset to first boot).

3. **Wait for readiness** (pings the health probe until it answers):

   ```sh
   for i in $(seq 1 30); do
     curl -fsS http://localhost:8080/readyz && break
     sleep 2
   done
   ```

   Prints the readiness report JSON (with `"status": "ok"`) once Postgres and
   Redis are healthy and the app has booted.

4. **Open http://localhost:8080 in your browser and sign in.** On first boot
   (empty `users` table), the username (3–32 chars of `[A-Za-z0-9_-]`) and
   password (≥ 10 chars) you type into the login form **create the admin** —
   that sign-in is the admin. API-first equivalent: the first
   `POST /api/v1/auth/login` mints the admin and returns the token.

That is the whole deployment: a signed-in app at `:8080` (the unified
Discover page is the default landing), with the SPA and the API served
same-origin by the single backend container.

The only required env vars are `MEDIARR_ENCRYPTION_KEY` (64 hex chars),
`MEDIARR_POSTGRES_DSN`, and `MEDIARR_REDIS_URL`; everything else has a
default. The compose file fills the DSN and Redis URL for you; see
`.env.example` and `docs/config.md` for the full surface.

## Optional development mode (source / UI work)

Everything in the quick start above is needed to **run** Mediarr. To work on
the **source** you need toolchains — this section is for contributors, not
deployers, and it does not apply to the container path above:

- **Node 22+** — UI source work (the Vite dev loop below). Also the toolchain
  of the image's `ui-build` stage (`deploy/Dockerfile`) and the `ui` CI job,
  but you do not need it to run the app.
- **Go 1.27+** — backend changes (`make build`, `make test`, the gates in
  `make preflight`). Docker is still required for `make dev` and the
  testcontainers integration tests.

### Vite dev loop (UI source work)

```sh
cd ui && npm ci && npm run dev   # Vite on :5173, proxies /api → :8080
```

Run it against the compose stack from the quick start (or `make dev`); point
the proxy at another instance with `MEDIARR_API_URL=http://host:port`.
When serving the backend from a local build instead of the image, set
`MEDIARR_UI_DIR` at your Vite dist (a missing/empty root 404s UI paths only —
API and health routes are unaffected; `docs/config.md`).

## Repository layout

```
cmd/mediarr/           the single Go binary (main + healthcheck subcommand)
internal/              backend packages: api, auth, config, dashboard,
                       domains/{movies,tv,music,books}, downloads, health,
                       indexers, metrics, paging, postgres (+migrations/),
                       providers, queue, redis, secrets,
                       services/{movies,tv,music,books}, settings, store
ui/                    Vite + React + TypeScript SPA (vitest; Playwright
                       browser e2e in ui/e2e/)
deploy/                Dockerfile (3 stages: Go build, Node 22 ui-build,
                       distroless runtime), compose.dev.yml,
                       compose.observability.yml, prometheus/, grafana/,
                       helm/ (chart + README + test.sh)
docs/                  PLAN.md, architecture.md, config.md, openapi.md,
                       observability.md, loadtest.md
test/                  e2e/ (pipeline E2E), loadtest/ (10k-item harness)
scripts/               ci-preflight.sh, push-gate.sh, release-notes.sh,
                       ui-smoke.sh (served-UI probes)
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
`POST /api/v1/auth/login`. Auth-gated: everything else — `auth/me`,
`dashboard`, `queue` (+ per-kind history) and the live `WS /api/v1/events`;
per-domain CRUD + `POST …/pipeline` for movies, TV (series/seasons/episodes),
music (artist/album/track), and books (author/title/edition); indexer
CRUD + test/stats; `search/*` and `profiles`; settings (download-client,
library overrides); plus the five-app parity surface (tags, quality/custom
formats, connect, calendar, import, blocklist, media management, the
Sonarr/Radarr/Lidarr/Prowlarr/Readarr drop-in migration endpoints) and
`GET /metrics`.

The full maintained route list is the **OpenAPI contract**:
[`openapi.yaml`](openapi.yaml) is the source of truth for every `/api/v1`
route and wire format, and [`docs/openapi.md`](docs/openapi.md) documents the
conventions and the lockstep gates (CI structural check, generated UI
types, and an e2e drift test that validates every operation against a live
server).

## Documentation

| Doc | Contents |
|---|---|
| `docs/PLAN.md` | Original design intent, milestones M0–M7 |
| `docs/architecture.md` | As-built architecture: components, module map, routes, auth, pipeline, data model, Redis usage, config & secrets, observability, deployment, testing |
| `docs/config.md` | Full environment-variable reference + defaults |
| `docs/openapi.md` | OpenAPI contract: wire-format conventions and lockstep gates |
| `docs/observability.md` | Metrics reference and Prometheus/Grafana wiring |
| `docs/loadtest.md` | Load-test methodology, thresholds, baseline |
| `RELEASE.md` | Release process (tags, cosign signing, deployment) |
| `SECURITY.md` | Supported versions, private vulnerability reporting, response SLA |
| `CONTRIBUTING.md` | Development loop, gates, and PR process |
| `CODE_OF_CONDUCT.md` | Contributor Covenant (v2.1) |
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
  **Trivy** image scan, SBOM, **cosign** signing of published images with the
  repo release key (`docs/cosign.pub`), a compose smoke test (including the
  served-UI probes in `scripts/ui-smoke.sh`), a Playwright browser E2E on the
  PR-local image (`ui-e2e`), and release-notes generation on tags.
