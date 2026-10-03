# Mediarr — Build Plan

> A single media-management service replacing the full ARR stack
> (Radarr + Sonarr + Lidarr + Readarr + Prowlarr), Postgres-backed,
> Docker-first, security-by-default.

## 1. Goals

- One service, four media types: movies, TV, music, books
- One indexer layer (the Prowlarr role) shared by all media types
- Full download pipeline: search → parse → quality match → download client → monitor → import → history
- Postgres for all durable state; Redis for cache, job queue, pub/sub, rate limiting
- Docker images for testing and local dev; production deploy via compose (Helm later)
- Code security + container best practices baked into CI, not bolted on

## 2. Non-goals (v1)

- Cloud-hosted multi-tenant service
- Mobile apps (API-first means a mobile app can come later for free)
- BitTorrent seeding management beyond standard download-client integration

## 3. Architecture

**Decision: single modular service (monolith with hard module boundaries).**
Rationale: the four media types share the pipeline; the ARR stack's only real
differences are metadata shape and quality rules. Separate services would mean
4x the operational surface for ~5% different logic.

```
                    ┌─────────────────────────────────────────┐
                    │              Mediarr service            │
  Web UI (SPA) ───► │  REST API + WebSocket events            │
                    ├─────────────────────────────────────────┤
                    │  Domains: movies | tv | music | books   │
                    │  (metadata model, quality/edition rules,│
                    │   import layout, search parsing)        │
                    ├─────────────────────────────────────────┤
                    │  Core: queue, workers, history, config  │
                    │  Indexers: definition store, test, cache│
                    │  Downloads: qBittorrent/SABnzbd/generic │
                    │  Providers: TMDB/TVDB/MusicBrainz/OL    │
                    ├─────────────────────────────────────────┤
                    │  Storage adapters: Postgres | Redis     │
                    └──────────────┬──────────────┬───────────┘
                                   │              │
                              ┌────▼────┐    ┌────▼────┐
                              │Postgres │    │  Redis  │
                              └─────────┘    └─────────┘
```

Layers (dependency point inward):
- `api/` — HTTP handlers, validation, auth (outermost)
- `services/` — use-cases per domain (add movie, start import, test indexer)
- `domain/` — pure business logic, no I/O (quality parsers, episode matchers)
- `adapters/` — Postgres, Redis, TMDB, download clients (outermost, swappable)

Background workers (in-process, supervised):
- Wanted/monitor scan (periodic + event-driven)
- Indexer search + result refresh
- Download queue watch → import trigger
- Import (parse client state, rename, move, artwork, metadata files)
- Metadata refresh (per-item + global schedule)
- Housekeeping (history purge, cache eviction, temp cleanup)

## 4. Data model (Postgres)

Migrations versioned with a real migration tool (e.g. golang-migrate).
No ORM-magic table creation — schema is code-reviewed.

Shared core tables:
- `media_items` (kind, external_id, name, paths, monitored, added_at) — or per-kind tables (see note)
- `wanted_items` (media_item_id, specifier — season/episode/album/edition, status)
- `queue` (job_id, media_item_id, indexer, download_client, state, progress)
- `history` (event, media_item_id, at)
- `indexers` (name, kind, base_url, api_key_encrypted, settings_json, enabled, last_test)
- `download_clients` (name, type, host, api_key_encrypted, settings_json)
- `settings` (key, value_json)
- `users` (id, username, password_hash, role) — present from day 1 even if v1 is single-user
- `api_tokens` (for UI + third-party access)

Per-kind tables (recommended over polymorphic blob):
- `movies` (tmdb_id, imdb_id, title, original_title, year, quality_profile_id)
- `series` / `episodes` (tvdb_id / tmdb_id, season, episode, title)
- `artists` / `albums` / `tracks` (musicbrainz ids, disc, track, quality_profile_id)
- `authors` / `books` / `editions` (openlibrary/isbn ids, edition, format)

Note: per-kind tables + a thin common interface beats one table with JSONB —
it keeps queryability, indexes, and FK integrity where it matters.

## 5. Redis usage (scoped, not a data lake)

- **Cache**: provider lookups (TTL 24h), indexer search results (TTL 10min)
- **Job queue**: Redis Streams for worker jobs (import, scan, refresh) with consumer groups — survives restarts, no lost jobs
- **Pub/sub**: event stream → WebSocket push to UI (download state, import done)
- **Rate limiting**: token bucket per provider + per indexer (token bucket via Redis Lua or client-side with Redis counters)

Rule: anything in Redis is **re-derivable from Postgres**. Redis is a cache,
never the source of truth.

## 6. Download pipeline (the core loop)

```
add/monitor item
   → create wanted entry
   → worker: search indexers (cached) → parse results
   → quality/edition match against profile
   → pick best → send to download client (torrent or usenet)
   → queue watch: poll client state
   → complete → import: rename (per-kind template), move,
     write metadata (.nfo/.emby if wanted), artwork, (music: checksum)
   → mark wanted satisfied → history
```

Per-kind specifics:
- **Movies**: single file, quality (resolution/codec/audio), scene-name parsing
- **TV**: series → season → episode; multi-episode episodes; release grouping
- **Music**: artist → album → track; lossless quality (flac/mp3 bitrate); optional checksum verify
- **Books**: title → edition; format (epub/mobi/azw3); author/series matching

## 7. Indexer layer (the Prowlarr role)

- Indexer definitions as code + user config (URL, key, enabled)
- Provider framework: one interface, adapters per protocol
  (TorrentRSS, Torznab, usenet NZB, PTP/Internet Archive where sensible)
- Built-in definition library (community JSON, same idea as Prowlarr)
- Indexer health: periodic test, error tracking, per-indexer stats
- Search fan-out: query N indexers concurrently, merge, rank, dedupe
- **Encrypted at rest** for indexer API keys (AES-256-GCM, key from env var —
  never logged, never in the image)

## 8. Security — code

- **Secrets**: env vars only (`.env.example` committed, `.env` gitignored);
  indexer/provider keys encrypted in DB with app-level AES-GCM
- **Auth**: Argon2id password hashing; API tokens (HMAC-signed, revocable);
  rate limiting on auth endpoints; CSRF protection on cookie flows;
  strict CORS (default: same-origin only)
- **Input**: strict validation at the API boundary (no ORM-level trust);
  path traversal hardening on import (resolved paths must stay under configured roots)
- **Logging**: structured JSON; redaction filter for anything matching
  `key`, `token`, `password` fields; no request-body logging of sensitive payloads
- **Dependency hygiene**: lockfile always committed; Dependabot/Renovate on
  major + minor; `govulncheck` in CI (or equivalent per language)
- **SAST**: Semgrep (security + correctness rulesets) in CI, gate on new findings
- **No shell execution** in the import path (use Go's os/exec or equivalent, no `sh -c`)
- **Time**: store UTC everywhere, convert at the edge

## 9. Security — container & supply chain

Build:
- Multi-stage Dockerfile; final image on `gcr.io/distroless/static` (or
  `scratch` + CA bundle) — no shell, no package manager, < ~15MB
- Non-root user (`USER 65532`)
- Pinned toolchain + module checksums; no `latest` anywhere
- SBOM generated (Syft) on every build; artifact retained in CI
- Image scanning with Trivy (failing on CRITICAL/HIGH with no fix)

Runtime (compose + Helm):
- Read-only root filesystem
- `no-new-privileges`, drop ALL capabilities, seccomp=runtime/default
- Ephemeral volumes only for configured data roots (media + downloads)
- No privileged ports, no host network
- Healthcheck (liveness: `/healthz`; readiness: `/readyz` checks DB+Redis)
- No `curl`/`bash` in the image → no interactive abuse surface

Supply chain:
- Reproducible builds: fixed GOFLAGS, no timestamps in build, pinned deps
- (Optional, later) cosign sign + verify
- Base image digest-pinned, not tag-pinned

## 10. Testing

| Layer | Tooling | Notes |
|---|---|---|
| Unit | standard test framework | domain logic 100% pure → fast, no mocks of I/O needed for most |
| Integration | **Testcontainers** (real Postgres + Redis) | no fakes for storage; migration up/down tested |
| API | HTTP test client | full request/response contract, error shapes |
| E2E (pipeline) | mock download client + mock indexer | add movie → fake download → assert import |
| Docker smoke | `docker build` + run + curl `/healthz` | CI job builds the test image and boots it |
| Security | `govulncheck`/`cargo-audit` + Trivy + Semgrep | CI gate |

Coverage target: ≥ 80% on `domain/` and `services/`; ≥ 60% overall.

## 11. CI/CD (GitHub Actions)

```
on: PR
  └─ lint + vet + semgrep + govulncheck
  └─ unit tests
  └─ integration tests (testcontainers: postgres:16, redis:7)
  └─ API tests

on: merge to main / tag
  └─ docker build (multi-arch: amd64, arm64)
  └─ SBOM (syft)
  └─ trivy scan (fail on CRITICAL)
  └─ push test image → ghcr.io/brandenk514/mediarr:sha-<short>
  └─ docker smoke test (run + /healthz + /readyz)
  └─ on tag: promote to :latest (or :vX.Y.Z)
```

Dev loop (local):
```
docker compose -f deploy/compose.dev.yml up
  → mediarr (build from source, watch mode)
  → postgres:16
  → redis:7
  → qemulator or qbittorrent (optional, for pipeline tests)
```

## 12. Repo layout

```
mediarr/
├── cmd/mediarr/          # entrypoint
├── internal/
│   ├── api/              # HTTP handlers, middleware, WS
│   ├── auth/             # users, tokens, argon2
│   ├── config/           # env parsing, validation, defaults
│   ├── core/
│   │   ├── queue/        # wanted, jobs, history
│   │   ├── workers/      # supervisor, stream consumers
│   │   └── events/       # pub/sub bus
│   ├── domains/
│   │   ├── movies/       # model, quality, parse, import
│   │   ├── tv/
│   │   ├── music/
│   │   └── books/
│   ├── indexers/         # provider framework + adapters
│   ├── downloads/        # client adapters (qbittorrent, sabnzbd)
│   ├── providers/        # tmdb, tvdb, musicbrainz, openlibrary
│   ├── postgres/         # store.Store impl + embedded migrations
│   └── redis/            # cache, streams, pubsub
├── deploy/
│   ├── Dockerfile
│   ├── compose.dev.yml
│   ├── compose.prod.yml
│   └── helm/             # (M7)
├── ui/                   # (M6) React SPA, separate build
├── test/
│   ├── e2e/              # pipeline smoke tests
│   └── fixtures/         # mock indexer, mock download client
├── .github/workflows/    # ci.yml, release.yml
├── .env.example
├── Makefile              # dev, test, build, docker, lint, scan
└── docs/
    ├── PLAN.md           # this file
    ├── architecture.md   # (M2)
    └── config.md         # (M4)
```

## 13. Milestones

| # | Milestone | Exit criteria |
|---|---|---|
| M0 | Scaffold | repo builds, Dockerfile works, CI green, `/healthz` + `/readyz`, Postgres+Redis via compose, auth stub (login → token) |
| M1 | Core + Movies | full movie pipeline (add → want → fake-indexer search → mock-client download → import) works end-to-end; unit + integration tests green |
| M2 | TV domain | series/season/episode; release parsing; multi-episode handling; pipeline tests |
| M3 | Music domain | artist/album/track; lossless quality; checksum verify; pipeline tests |
| M4 | Books domain | author/title/edition; format rules; pipeline tests; config reference doc |
| M5 | Real indexers + clients | 5+ working indexer adapters; qBittorrent + SABnzbd adapters; indexer health UI endpoints |
| M6 | Web UI | React SPA: dashboard, per-kind list, add flows, queue view, settings |
| M7 | Hardening | load test (10k items, sustained import), observability (Prometheus metrics, Grafana dashboards), Helm chart, release process |

M1 is the spike that proves the architecture. If the modular approach
struggles at M1, we pivot before M2 — not after M4.

## 14. Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Scene-name parsing (movies/TV) is a deep rabbit hole | High | Phase it: M1 uses a simple regex table; expand from real-world failures, not upfront |
| Provider API changes (TMDB, TVDB) | Medium | Adapter layer + integration tests against recorded fixtures; no direct calls in domain code |
| Scope creep toward "perfect parity" | High | M1 spike gates the architecture; parity is a feature list, not a goal |
| Single-service complexity | Medium | Hard module boundaries + interface-based testing keep domains independent; can split later if needed |
| GPL v3 licensing implications | Low | Compatible with all ARR deps (also GPL); no proprietary components allowed — keep deps permissive or GPL |

## 15. Decisions (confirmed)

1. **Language**: Go — static single binary, ideal for distroless images
2. **UI timing**: API-first — UI at M6 after all four media types work
3. **Multi-user**: single-user v1; `users` table + roles in schema from M0
4. **Book formats**: epub + mobi + azw3 (full Readarr parity)
5. **Audio checksum verify**: on by default, opt-out per library
