# Mediarr — Architecture

> This document is the **as-built** architecture overview, carried past M2:
> it reflects the five-app parity work (P1–P7), the Books metadata module,
> the v0.3.0 containerized UI (built UI shipped in the image and served
> same-origin, #86/#87/#88), and the v0.4.0 unified app shell
> (#115–#123, #126, #139). For the original design intent and milestone
> history, read [`docs/PLAN.md`](PLAN.md); for the full environment-variable
> reference, [`docs/config.md`](config.md); for the wire-format contract,
> [`docs/openapi.md`](openapi.md).

## 1. System overview

Mediarr is a single-operator media manager for **movies, TV, music, and books**,
built as one Go binary plus a browser UI.

- **Backend:** one Go binary (`cmd/mediarr`, Go 1.27 — `go.mod` is `go 1.27.4`,
  CI builds with 1.27.4, module `github.com/brandenk514/mediarr`). The binary
  starts Postgres and Redis clients, applies SQL migrations, and serves the
  HTTP API, the auth endpoints, `/metrics`, and the health probes from a single
  `net/http` `ServeMux`. It also supports a `mediarr healthcheck` subcommand
  (ping Postgres + Redis, exit 0/1) used as the container healthcheck.
- **Frontend:** a Vite/React/TypeScript SPA in `ui/`. It talks to the backend
  over same-origin relative URLs; in development the Vite dev server (`:5173`)
  proxies `/api` to the backend (`:8080`).
- **Databases:**
  - **Postgres 16** is the source of truth — media metadata, wants, the
    download queue, history, users/tokens, settings, and indexer definitions.
    Everything lives in Postgres; nothing is derivable only from Redis.
  - **Redis 7** is scoped: a TTL cache for indexer search results, a pub/sub
    event bus for live UI updates, and a Redis-Streams job queue that is built
    and exposed but not yet consumed by a worker (reserved capacity, PLAN §5).
- **Download clients (optional, external):** qBittorrent (torrents) and
  SABnzbd (NZBs) are reached through adapters in `internal/downloads`; the
  default `mock` client simulates a download so the pipeline runs with no
  external client.

There are no background workers in the as-built system beyond one periodic loop
(the indexer health worker, §6). Media pipelines are **on-demand**: the UI
(operators) drives them with explicit `POST …/pipeline` calls.

## 2. Component diagram

```
                ┌──────────────────────────────────────────────────────────┐
                │                mediarr (single Go binary)               │
                │                                                        │
  browser       │  ┌────────────────────────────────────────────────┐    │
  (React/TS     │  │ HTTP server (net/http ServeMux, :8080)         │    │
  SPA — in dev  │  │  /api/v1/...   Bearer-auth + WS /api/v1/events │    │
  from Vite     │  │  / + /assets/...  built UI, same-origin (#87)  │    │
  :5173, or in  │  │  (served from the image, #88)                  │    │
  the image) ───┼──►│  /healthz  /readyz  /version  /metrics         │    │
       │       │  └────────────────────────────────────────────────┘    │
                │            ▼              ▼              ▼            │
                │  ┌────────────────┐ ┌──────────────┐ ┌──────────────┐ │
                │  │ indexers/      │ │ downloads/   │ │ providers/   │ │
                │  │ torznab,       │ │ mock,        │ │ tmdb, tvdb,  │ │
                │  │ torrent-rss,   │ │ qbittorrent, │ │ musicbrainz, │ │
                │  │ nzb, fake (dev)│ │ sabnzbd      │ │ openlibrary  │ │
                │  │ fanout + cache │ │ factory      │ │ (metadata)   │ │
                │  │ + health worker│ └──────┬───────┘ └──────────────┘ │
                │  └──────┬─────────┘        │                          │
                │         │ vault-encrypt    │ external HTTP:           │
                │         ▼                  ▼                          │
                │  ┌────────────┐   qBittorrent / SABnzbd (optional)    │
                │  │ secrets/   │   (default client is mock)            │
                │  │ AES-256-GCM│                                      │
                │  │ vault      │                                      │
                │  └────────────┘                                      │
                │            ┌──────────────────────────────────────┐   │
                │            │ Postgres store (internal/postgres)   │   │
                │            │ users, api_tokens, settings, indexers│   │
                │            │ + per-domain catalog/wanted/queue/   │   │
                │            │ history tables (migrations 0001-0033)│   │
                │            └──────────────────────────────────────┘   │
                │            ┌──────────────────────────────────────┐   │
                │            │ Redis (internal/redis)               │   │
                │            │ search cache (TTL 10m) · pub/sub bus │   │
                │            │ · streams job queue (reserved)       │   │
                │            └──────────────────────────────────────┘   │
                └──────────────────────────┬───────────────────────────┘
                                           │
              ┌────────────────────────────┼─────────────────────────────┐
              ▼                            ▼                             ▼
      Postgres 16                    Redis 7                     optional
      (compose / Helm, :5432)       (compose / Helm, :6379)     qBittorrent /
                                                                          SABnzbd

  Prometheus ──► /metrics (Bearer via bearer_token_file)
  Grafana     ──► dashboards (deploy/prometheus, deploy/grafana,
                  deploy/compose.observability.yml)
```

## 3. Module map

### `cmd/`

| Package | Role |
|---|---|
| `cmd/mediarr` | The single binary. Loads config, builds the secrets vault and Postgres store, applies migrations (`pg.Migrate(ctx)`), connects Redis (cache, event bus, streams queue), builds the four domain services (movies/tv/music/books), the indexer registry + fanout, the download-client factory, the API server with auth middleware, and the health checker. Owns graceful shutdown (SIGINT/SIGTERM → `http.Server.Shutdown` with `MEDIARR_SHUTDOWN_TIMEOUT`). Also runs the indexer health worker as the only periodic background loop. |

### `internal/`

| Package | Role |
|---|---|
| `api` | The HTTP layer: route registration (`server.go`), middleware (`requestID`, `recoverPanic`, per-route request logging), and handler files per area (`auth`, `movies`, `tv`, `music`, `books`, `queue`, `dashboard`, `search`, `settings`, `indexers`, `events`, `metrics`). Auth-gated routes use `requireAuth` (Bearer token). |
| `auth` | Single-user authentication. Argon2id password hashing, opaque `ma_`-prefixed API tokens stored as salted SHA-256 fingerprints (revocable), first-boot admin minting, token lifecycle (create, list, check, revoke). Owns its Postgres adapter: `auth.Repo` (users + `api_tokens`) and the `UserStore` interface. |
| `config` | Pure function `Load() (Config, error)`: reads the `MEDIARR_*` environment variables, applies defaults, validates (e.g. the 64-hex `MEDIARR_ENCRYPTION_KEY`). No I/O, no hidden defaults — the full surface is in `docs/config.md`. |
| `dashboard` | The `GET /api/v1/dashboard` read model: per-domain counts (monitored items, pending wants, queue by state, recent history + recent queue) across all four kinds. Defines the `Repo` interface; `postgres.DashboardRepo` implements it with a fixed set of aggregate queries (handful of round-trips regardless of library size — never loads whole tables). |
| `domains/movies`, `domains/tv`, `domains/music`, `domains/books` | Domain logic per media type: quality/format profiles and release-name parsing (e.g. `movies/quality.go`, `books/format.go`), wanted-row semantics, and per-domain layout rules used by import. |
| `downloads` | Download-client abstraction: `Client` interface, `Factory` (kind `mock` / `qbittorrent` / `sabnzbd`; built at startup from `MEDIARR_DOWNLOAD_CLIENT` + the `MEDIARR_QB_*`/`MEDIARR_SAB_*` env vars), per-client adapters, a `Send`/`Poll` flow, and redaction helpers for credentials in logs. |
| `health` | Liveness (`/healthz`), readiness (`/readyz` — pings Postgres and Redis with a 2s default timeout), and build-info (`/version`, ldflags-stamped, overridable in tests). |
| `indexers` | Indexer adapters: `torznab`, `torrent-rss`, `nzb`, and a development-only `fake` source; `Fanout` (concurrent multi-indexer search with per-indexer timeout and deterministic merge/dedupe); per-indexer token-bucket rate limiting (`ratelimit.go`, in-process); health tracking (`health.go`, `health_worker.go`); `redact.go` for key-safe logging. The registry holds enabled/defined indexers from Postgres with vault-encrypted keys. |
| `metrics` | Prometheus collectors: counters/histograms for HTTP, search, and pipeline operations, plus scrape-time collectors (`mediarr_queue_depth`, `mediarr_library_items`, `mediarr_dependency_up`). Mounted at `/metrics`. |
| `paging` | Shared `Page`/`PageSize` query-parameter parsing with defaults/caps for list endpoints. |
| `postgres` | `*sql.DB` repositories: `MovieRepo`, `TVRepo`, `MusicRepo`, `BooksRepo` (catalogs), `QueueRepo` (the four `queue`/`*_queue` table variants, dispatched by kind), `DashboardRepo`, `SettingsRepo` (vault-sealed values), `IndexersRepo` (vault-encrypted keys), plus `migrations/` (embedded SQL, applied in filename order, tracked in `schema_migrations`). |
| `providers` | External metadata adapters: TMDB (movies/TV), TVDB (TV), MusicBrainz (music), OpenLibrary (books) — used by the `GET /api/v1/search/…` endpoints and by the add flows. |
| `queue` | Shared queue-model types: `Kind` (movie/tv/music/book), `State` (queued/downloading/complete/failed), row types, and the `Repo` interface implemented by `postgres.QueueRepo`. |
| `redis` | Thin client over go-redis: `Client` (cache with TTL), `Events` (pub/sub bus), and `Queue` (Redis Streams, consumer group `mediarr`). |
| `secrets` | AES-256-GCM `Vault` keyed by `MEDIARR_ENCRYPTION_KEY` (32 bytes / 64 hex chars). Ciphertext = `[12-byte nonce][ciphertext][16-byte tag]`. The key is never persisted, logged, or exported. Applied at the two persistence boundaries: `postgres.IndexersRepo` (indexer API keys) and `postgres.SettingsRepo` (download-client credentials). |
| `services/movies`, `services/tv`, `services/music`, `services/books` | The four domain services. Each implements the shared pipeline: add/monitor items, create wanted rows, run the download pipeline (search → match → send → poll → import), and publish `queue.updated` / `history.appended` events. `movies` is the reference implementation; `tv`, `music`, and `books` add domain-specific entities (series/episodes, artists/albums/tracks, authors/titles/editions). |
| `settings` | Typed settings: the `DownloadClient` struct (kind + qBittorrent/SABnzbd connection fields, credentials vault-sealed at rest) and per-domain library overrides. Backed by the Postgres `settings` table. |
| `store` | Store-level interfaces (`Store`, `Cache`, `Events`, `Queue`, `Event`) that decouple the services from `internal/postgres` and `internal/redis` — the seams unit tests fake. |

### `ui/`

The Vite/React/TypeScript SPA (vitest unit tests; Playwright browser e2e in
`ui/e2e/`). As-built for the v0.4.0 unified shell:

- **Single source of truth for the shell.** `src/nav.ts` holds every route
  path (`PATHS`) and the eight sidebar sections (`SECTIONS`): Dashboard,
  Discover, Library, Calendar, Activity, Indexers, Settings, System. The
  sidebar rail (`src/pages/Layout.tsx`) and the route tree (`src/App.tsx`)
  both derive from it, so rail and routes cannot drift (#116).
- **Routing model.** `/` is a redirect to the unified cross-kind
  **Discover** page (`/discover`) — the default landing (supersedes the
  pre-v0.4.0 Movies-list landing, #118). **Library** is the unified
  cross-kind grid at `/library`; kind is a facet, not a switch — the
  Library section's four kind rows deep-link the grid to
  `/library?kind=tv|movie|music|book` (#119). The per-kind list pages
  (`/tv`, `/movies`, `/music`, `/books`) and all deep pages (`/movies/add`,
  `/tv/:id`, `/music/:id`, `/books/:id`, collections/discover/calendar,
  unmapped files, bookshelves/series) stay fully routed, reachable by direct
  URL and via the rail sub-links. Activity routes live at
  `/activity/{queue,history,blocklist}` (with the legacy `/queue` alias),
  Wanted at `/wanted/{missing,cutoff}`, the Indexers hub at `/indexers`
  (with `/search` = manual release search, `/history` = search history,
  `/indexers/stats`), and Settings/System under `/settings/:section` and
  `/system/:page` (both no-section roots redirect to their default
  sub-page).
- **Global search (#117).** The top-bar search is cross-kind and
  deep-linkable: it navigates to `/search/all?q=…`, a first-class results
  route that fans out client-side to the four per-kind search endpoints and
  renders one unified grid with per-kind badges. It is distinct from
  `/search` (the Indexers manual release-search page).
- **Unified media detail + integrated add flows (#120).** The four detail
  pages share one detail shell (header, badges, toolbar, files), and the
  per-kind add flows are integrated into the detail toolbars (Add Episode on
  the series page, Add Release on the artist page, Add Edition on the author
  page) alongside the standalone add pages.
- **Responsive (#123).** The shell is responsive down to phone widths: a
  fixed overlay nav drawer reuses the same rail (identical `rail-*`
  selectors), and the detail toolbar scrolls on narrow screens instead of
  widening the page.
- **UTC anchoring (#139).** Calendar windows and queue/episode status
  derivations are anchored to the UTC date so a day's releases and statuses
  do not shift with the browser's local timezone.
- **Live events (#126).** The UI consumes `WS /api/v1/events` (the
  backend's WebSocket bridge, migrated from gorilla/websocket to
  coder/websocket) for live queue/history redraws.
- **Auth in the browser.** The token lives in page memory only (never
  `localStorage`/`sessionStorage`/cookies); the WebSocket accepts it as
  `?token=`. See §5.

## 4. HTTP API

All routes come from `internal/api/server.go` (plus the conditional registrations
in `indexers.go`, `metrics.go`, and `events.go`). Middleware order: `requestID`
→ `recoverPanic` → route logging. Public routes:

```
GET  /healthz                    liveness (no dependencies)
GET  /readyz                     readiness (Postgres + Redis ping)
GET  /version                    build info JSON
POST /api/v1/auth/login          login; mints the admin on first boot
```

Auth-gated routes (all `Authorization: Bearer <token>`; the WebSocket may also
take `?token=` because browsers cannot set headers on the WS handshake):

```
Auth / dashboard
  GET  /api/v1/auth/me
  GET  /api/v1/dashboard
  GET  /api/v1/queue                        ?kind&state&q&page&page_size
  GET  /api/v1/queue/{kind}/{parent}/history
  GET  /api/v1/events  (WebSocket upgrade)  live queue.updated / history.appended

Movies
  POST /api/v1/movies
  GET  /api/v1/movies
  GET  /api/v1/movies/{id}
  PUT  /api/v1/movies/{id}
  POST /api/v1/movies/{id}/pipeline

TV
  POST /api/v1/tv
  GET  /api/v1/tv
  GET  /api/v1/tv/{id}
  PUT  /api/v1/tv/{id}
  PUT  /api/v1/tv/{id}/seasons/{season}
  PUT  /api/v1/tv/{id}/episodes/{season}/{episode}
  POST /api/v1/tv/{id}/episodes
  GET  /api/v1/tv/{id}/episodes
  GET  /api/v1/tv/{id}/wanted
  POST /api/v1/tv/{id}/pipeline

Music
  POST /api/v1/music/artists
  GET  /api/v1/music/artists
  GET  /api/v1/music/artists/{id}
  PUT  /api/v1/music/artists/{id}
  POST /api/v1/music/artists/{id}/albums
  GET  /api/v1/music/artists/{id}/albums
  GET  /api/v1/music/albums/{id}
  PUT  /api/v1/music/albums/{id}
  POST /api/v1/music/albums/{id}/tracks
  GET  /api/v1/music/albums/{id}/tracks
  PUT  /api/v1/music/albums/{id}/tracks/{disc}/{number}
  GET  /api/v1/music/albums/{id}/wanted
  POST /api/v1/music/artists/{id}/pipeline

Books
  POST /api/v1/books/authors
  GET  /api/v1/books/authors
  GET  /api/v1/books/authors/{id}
  PUT  /api/v1/books/authors/{id}
  POST /api/v1/books/authors/{id}/titles
  GET  /api/v1/books/authors/{id}/titles
  GET  /api/v1/books/titles/{id}
  PUT  /api/v1/books/titles/{id}
  POST /api/v1/books/titles/{id}/editions
  GET  /api/v1/books/titles/{id}/editions
  GET  /api/v1/books/editions/{id}
  PUT  /api/v1/books/editions/{id}
  GET  /api/v1/books/titles/{id}/wanted
  POST /api/v1/books/authors/{id}/pipeline

Indexers
  GET    /api/v1/indexers
  POST   /api/v1/indexers
  PUT    /api/v1/indexers/{id}
  PATCH  /api/v1/indexers/{id}/enabled
  DELETE /api/v1/indexers/{id}
  POST   /api/v1/indexers/{id}/test
  GET    /api/v1/indexers/{id}/stats

Search / add-flow
  GET  /api/v1/search/movies?query=
  GET  /api/v1/search/tv?query=
  GET  /api/v1/search/tv-tmdb?query=
  GET  /api/v1/search/artists?query=
  GET  /api/v1/search/books/authors?query=
  GET  /api/v1/search/books/works?query=
  GET  /api/v1/profiles?kind=           built-in profiles (+ default) per kind

Settings
  GET  /api/v1/settings/download-client
  PUT  /api/v1/settings/download-client
  GET  /api/v1/settings/library
  PUT  /api/v1/settings/library/{kind}

Observability
  GET  /metrics                    (registered when a metrics registry exists)
```

Notes on registration:
- The four indexer **write** routes are only registered when the indexer
  registry is non-nil (guarded inside `SetIndexers`) — in `cmd/mediarr` it
  always is.
- `/metrics` is only registered when a metrics registry is provided
  (`SetMetrics` with non-nil `MetricsDeps.M`); `cmd/mediarr` always provides one.
- `/api/v1/events` is registered directly on the mux (WebSocket upgrade), so it
  bypasses the per-route HTTP metrics wrapper.

## 5. Auth model

Single-user, token-based (PLAN §6, as built):

- **First boot.** When the `users` table is empty, the first
  `POST /api/v1/auth/login` call **creates the admin**: username 3–32 chars of
  `[A-Za-z0-9_-]`, password ≥ 10 chars. Subsequent logins while a user exists
  are ordinary logins. (This is the single-user gate — it applies regardless of
  the `MEDIARR_AUTH_PINNING` setting, which is parsed but not yet enforced by
  the code; see `docs/config.md`.)
- **Passwords** are hashed with Argon2id (`internal/auth`).
- **API tokens** are opaque, minted with the `ma_` prefix, stored only as a
  salted SHA-256 fingerprint (never the raw token), and revocable via the API.
  The token is the UI's only credential: it lives **in memory only** — never
  in `localStorage`, `sessionStorage`, or cookies (an explicit UI acceptance
  criterion) — and is sent as `Authorization: *** on every request. The
  WebSocket accepts it as `?token=` as well, because browsers cannot set the
  `Authorization` header on the WS handshake.
- **`MEDIARR_ENCRYPTION_KEY`** (64 hex chars) is the only secret the process
  holds: it drives the AES-256-GCM vault used for indexer API keys and
  download-client credentials at rest.

## 6. Download pipeline (core loop)

The pipeline is the heart of the system and is implemented per domain
(`internal/services/{movies,tv,music,books}`). The as-built trigger model is
**operator-driven**: the UI calls `POST …/pipeline` for a monitored item; the
pipeline then runs synchronously end-to-end and streams progress through
`queue.updated` / `history.appended` events.

1. **Add & monitor.** `POST /api/v1/movies` (and TV/music/book equivalents)
   creates the catalog row. Turning monitoring on (or adding a monitored item)
   creates a `wanted` row (`status=pending`) — the row is the unit of work.
2. **Search (fanout).** The service builds a search query from the domain's
   parsed release-name conventions and calls the indexer `Fanout`, which
   searches all **enabled** indexers concurrently (per-indexer timeout; one
   failing indexer never fails the search). A per-domain development `fake`
   indexer source keeps the pipeline testable offline. Results are cached in
   Redis for 10 minutes (`indexers.CacheSearcher`), so repeated runs are cheap.
3. **Match.** Results are parsed with the domain's profile/format parser
   (`domains/*`), filtered against the item's quality/format profile
   (e.g. `HD-1080p` for movies, `Lossless` for albums, `Best` for books), and
   the best release is picked.
4. **Send.** The pick is inserted into the domain's queue table
   (`queue`, `tv_queue`, `music_queue`, `book_queue`) in state `queued`, then
   handed to the download client via the `downloads.Client` interface
   (`Add`). The `Factory` selects the client kind — `mock` (default; simulates
   a complete download), `qbittorrent` (Web API), or `sabnzbd` (JSON API) —
   from `MEDIARR_DOWNLOAD_CLIENT` at startup (see §9); the UI's settings
   endpoint persists the same fields for the UI.
5. **Poll & import.** The pipeline polls `Status` until the client reports
   complete, then **imports**: the file is placed under the domain's media root
   (`MEDIARR_MEDIA_ROOT`, with per-domain derivation — movies use it directly,
   TV/music/books nest under it; see `docs/config.md` §3) with the domain's
   folder layout. Books and music additionally checksum/verify the imported
   file against the release record.
6. **Satisfy.** On success the `wanted` row becomes `satisfied`, the queue row
   `complete`, a `history` row is appended, and both `queue.updated` and
   `history.appended` events are published to the Redis bus → the UI's
   WebSocket redraws the queue and history cards.

Failure at any stage marks the queue row `failed` with a history entry; a
re-run of the pipeline retries the item.

### The only periodic loop: the indexer health worker

At startup the service layer runs an initial health sweep over all enabled
indexers (a lightweight probe per kind: Torznab XML, torrent-RSS feed, NZB
source), recording `last_test` / last-success against the `indexers` table.
If `MEDIARR_INDEXER_HEALTH_INTERVAL` is set to a positive duration, the sweep
repeats on that interval; otherwise (unset or `0`, which is the actual default
despite the comment in `internal/config/config.go` claiming `5m`) it runs once
and stops, and `POST /api/v1/indexers/{id}/test` remains the on-demand path.
Health stats are exposed via `GET /api/v1/indexers/{id}/stats`.

## 7. Postgres data model

Migrations live in `internal/postgres/migrations/` (embedded SQL, applied in
filename order, tracked in `schema_migrations`; the `pgcrypto` extension is
required). The schema spans **33 migrations** (0001–0033): the core
M0–M2 schema below (0001–0007) plus 0008–0033, which added the five-app
parity work — root folders (with per-root default profiles and the Calibre
link), the extended item fields (tags, series type/season folders, movie
editions/collections/release date, music covers/labels/monitoring, book
monitoring), the blocklist and search/import activity tables, the indexer
hub (definitions, proxies, tags), the download-client registry, and
notifications — and the Books metadata module (B1–B6, MT2/MT3). The table
below documents the core schema; the migrations directory itself is the
authoritative as-built schema.

| Migration | Tables | Purpose |
|---|---|---|
| `0001_initial_schema` | `users`, `api_tokens`, `settings`, `schema_migrations` | Auth (Argon2id hash, salted token fingerprints), key-value `settings` (JSON `value`), migration bookkeeping; enables the `pgcrypto` extension |
| `0002_movies` | `movies`, `wanted`, `queue`, `history` | Movie catalog (UNIQUE title+year, default profile `HD-1080p`), one wanted row per movie, download jobs (`queued→downloading→complete/failed`), append-only history |
| `0003_tv` | `tv_series`, `tv_episodes`, `tv_wanted`, `tv_queue`, `tv_history` | Series/episode tree with per-episode monitoring and season-pack semantics |
| `0004_music` | `music_artists`, `music_albums`, `music_tracks`, `music_wanted`, `music_queue`, `music_history` | Artist → album → track tree, default profile `Lossless` |
| `0005_books` | `book_authors`, `book_titles`, `book_editions` | Author → title → edition tree |
| `0006_books_pipeline` | `book_wanted`, `book_queue`, `book_history` | Book pipeline rows |
| `0007_indexers` | `indexers` | Indexer definitions: `kind`, `base_url`, **`api_key_encrypted`** (vault ciphertext; NULL = no key), free-form `settings_json` (protocol options — no per-adapter migration), `enabled`, `last_test` |
| `0008`–`0033` | (parity + Books metadata, see above) | Root folders, extended item fields, blocklist + search/import activity, indexer hub (definitions/proxies/tags), download clients, notifications, Books B1–B6 + MT2/MT3 |

Conventions:
- **Per-domain prefixed tables** (`tv_*`, `music_*`, `book_*`) keep the four
  pipelines in separate table families; the shared `queue`/`wanted`/`history`
  tables are the movie originals. `postgres.QueueRepo` maps `queue.Kind`
  (`movie`/`tv`/`music`/`book`) to the right table pair — the four queue tables
  share identical column shapes so the queries stay symmetric.
- **Everything re-derivable.** Postgres is the only durable source of truth;
  Redis holds only cache and events.

## 8. Redis usage

Scoped per PLAN §5 — Redis is never the source of truth:

- **Search cache.** Indexer search results, TTL 10 minutes
  (`indexers.CacheSearcher` over `redis.Client`).
- **Event bus.** Redis pub/sub with two channels — `mediarr:queue.updated` and
  `mediarr:history.appended`. Services publish; the WebSocket endpoint
  (`/api/v1/events`) subscribes and streams matching events as JSON frames
  (a `hello` frame first so the client knows the socket is live).
- **Job queue (reserved).** A Redis-Streams queue with consumer group `mediarr`
  (`redis.Queue`, wired in `main.go`) exists for worker-driven pipelines per
  PLAN §5, but no worker consumes it yet — pipelines are on-demand today (§6).
- **Not used for rate limiting.** Per-indexer rate limits are in-process token
  buckets (`indexers/ratelimit.go`), not Redis keys.

## 9. Config & secrets

- **Config is env-only** (`internal/config/config.go`, a pure `Load()`): the
  three required vars (`MEDIARR_ENCRYPTION_KEY`, `MEDIARR_POSTGRES_DSN`,
  `MEDIARR_REDIS_URL`) plus ~30 optionals with defaults. Full reference:
  `docs/config.md`.
- **Vault encryption at rest** (`internal/secrets`): AES-256-GCM keyed by
  `MEDIARR_ENCRYPTION_KEY`. Two boundaries encrypt: `postgres.IndexersRepo`
  (indexer API keys → `api_key_encrypted`) and `postgres.SettingsRepo`
  (qBittorrent/SABnzbd credentials inside `settings`). A NULL encrypted
  column means "no key" (not an empty key).
- **Download-client bootstrap:** the client kind and connection fields are
  read from env at startup (`MEDIARR_DOWNLOAD_CLIENT` plus `MEDIARR_QB_*` /
  `MEDIARR_SAB_*`) and the factory builds the single client all four pipelines
  share. The UI's `GET/PUT /api/v1/settings/download-client` persists the same
  fields (same names) in Postgres with credentials vault-sealed — it is the
  durable settings surface the UI renders.

## 10. Observability

- **Probes:** `GET /healthz` (process alive), `GET /readyz` (Postgres + Redis
  reachable; 2s default dependency timeout), `GET /version` (ldflags-stamped
  build info — version, commit, build metadata).
- **Metrics:** `GET /metrics` (Prometheus text format, Bearer-auth like every
  API route) served by `internal/metrics`. Counters/histograms cover HTTP
  requests, indexer searches (with per-indexer labels), and pipeline runs
  (per-domain success/failure/latency); scrape-time collectors add
  `mediarr_queue_depth`, `mediarr_library_items`, and `mediarr_dependency_up`.
  See `docs/observability.md` for the full metric list.
- **Prometheus & Grafana:** `deploy/prometheus/prometheus.yml` scrapes the
  backend at `:8080/metrics` using a `bearer_token_file` (put a token minted
  via `POST /api/v1/auth/login` in a file and mount it — see the comments in
  the yml), and `deploy/grafana/provisioning/` (datasource + dashboards) run as
  a separate stack:
  `docker compose -f deploy/compose.dev.yml -f deploy/compose.observability.yml up --build`
  (see `docs/observability.md`).
- **Helm:** the chart does not bundle a Prometheus deployment; on-cluster
  scraping of `/metrics` uses the same bearer-token pattern as
  `deploy/prometheus/prometheus.yml` (see `deploy/helm/README.md` for the
  chart's values and security model).
- **Logging:** structured `log/slog` JSON; the API logs each request with the
  request ID; indexer and download-client code redact credentials
  (`indexers/redact.go`, `downloads` redaction).

## 11. Deployment

- **Dockerfile** (`deploy/Dockerfile`): three stages —
  - `golang:1.27.4-bookworm` build: module + build-cache mounts,
    `CGO_ENABLED=0`, `-trimpath`, ldflags-stamped version/commit/build time,
    and pre-created writable dirs (`/writables/downloads`, `/writables/media`)
    so seeded named volumes inherit the app user's ownership.
  - `node:22-bookworm` ui-build (#88): `npm ci` strictly from the committed
    `ui/package-lock.json` (the single source of npm dependencies), then
    `npm run build` (`tsc -b` + `vite build`). Only the finished `dist`
    crosses into the runtime stage — node, npm, `node_modules`, and the rest
    of the toolchain stay in the build stage. Node 22 matches the `ui` CI job
    and the golang bookworm family.
  - `distroless/static-debian12:nonroot` runtime: the `/mediarr` binary,
    `/downloads` + `/media` as the only writable paths (chowned
    `nonroot:nonroot` so mounted volumes are writable by the app), and the
    complete built UI at **`/home/nonroot/ui`** (index.html + hashed assets +
    `public/` files) — read-only on the rootfs, owned by the app user. No
    shell, no package manager. `EXPOSE 8080`. Healthcheck via the binary's
    own `healthcheck` subcommand (pings Postgres + Redis, exit 0/1).
- **Same-origin UI serving (#86 decision A, shipped in #87).** The built UI
  ships in the existing `mediarr` image and is served by the same Go server
  on `:8080` — the maintainer's recorded hosting decision (2026-11-13, #86).
  The serving contract lives in `internal/api/ui.go`: the UI catch-all
  (`GET/HEAD` only) is wired as the lowest-priority `/` route, after every
  API/health/metrics route, and serves the Vite dist from `MEDIARR_UI_DIR`
  (default `/home/nonroot/ui`, the #88 image contract path) with SPA
  fallback for client-side routing. Reserved route families (`/api`,
  `/healthz`, `/readyz`, `/version`, `/metrics` — checked at path-segment
  boundaries, so `/apiary` and `/apiv2` are UI paths, not API) never fall
  through: unknown `/api` paths answer a JSON 404, never index.html. A
  missing file-like path is a 404, not the SPA; directories are never
  listed; every open goes through `os.OpenInRoot` with segment validation
  (no traversal or symlink escape). A missing/empty asset root 404s UI paths
  only — the dev-without-a-build case — and leaves API/health routes
  unaffected. The historical alternative B (a separate frontend compose
  service) is retained in #86 as context only; it is no longer the planning
  basis.
- **Compose** (`deploy/compose.dev.yml`, project `mediarr-dev`): the
  operator quick start (README "Quick start"; #89) — **one stack is the whole
  app**: `mediarr:dev` (built from source by default), `postgres:16-alpine`,
  `redis:7-alpine`, with the backend the only published port (`8080:8080`)
  serving both the API and the built UI. The backend runs with a **read-only
  rootfs** (plus a `/tmp` tmpfs), `cap_drop: ALL`, `no-new-privileges:
  true`, and the only writable volumes (`/media`, `/downloads` — the
  mock-client scratch and imported media). It declares `depends_on:
  condition: service_healthy` for both datastores and is health-checked
  itself via the `healthcheck` subcommand. The compose file fills in the
  `MEDIARR_*` env vars (including a zeroed placeholder
  `MEDIARR_ENCRYPTION_KEY` — override it for real use). For releases, the
  same acceptance flow runs the **published image** instead of the source
  build (RELEASE.md, "Browser verification").
- **Security model (shipped).** One distroless static image (no shell),
  non-root user uid/gid 65532 (baked into the base image), read-only rootfs,
  the only writable paths the declared media volumes, and the only
  published-port surface `:8080` — the #86 decision keeps the UI inside that
  one surface (no second service, no new ports, no second image to scan or
  sign). Secrets never touch disk: `MEDIARR_ENCRYPTION_KEY` from the
  environment, AES-256-GCM vault for indexer/download-client credentials at
  rest. Helm (`deploy/helm/mediarr`) deploys the same dev-parity model into
  Kubernetes.
- **Helm** (`deploy/helm/mediarr`): deploys the same dev-parity stack into
  Kubernetes — `mediarr` (Deployment + Service, `/media` + `/downloads` as
  emptyDir in this dev chart), Postgres 16 (StatefulSet with a 1 Gi PVC via
  `volumeClaimTemplates`, `postgres.persistence.*`), Redis 7 (Deployment;
  emptyDir by default, `redis.persistence.enabled=true` for a PVC), and the
  chart Secret (chart-managed, or `existingSecret: <name>` mode). Every pod
  carries the dev-parity security model from `deploy/helm/README.md`: nonroot
  user, read-only rootfs, `cap_drop: ALL`, `no-new-privileges`,
  `allowPrivilegeEscalation: false`, seccomp `RuntimeDefault`, and
  liveness/readiness probes. Validated by `deploy/helm/test.sh` (lint +
  render assertions); a live `helm install` on a kind cluster is part of the
  M7 release validation (see `RELEASE.md`).
- **Release process:** see `RELEASE.md` (root of the repo) — tag-driven
  release, cosign-signing of the image with the repo release key
  (`docs/cosign.pub`), the browser UI verification step, and the
  kind install/verify steps.

## 12. Testing strategy

- **Unit tests** — per-package `*_test.go` (stdlib `testing` + `testify`
  assertions). External dependencies are faked at the `internal/store`
  interfaces (fake Postgres/Redis/store) and with `httptest` servers for
  indexer/provider/download-client adapters (e.g. the downloads adapters run
  against in-process `httptest` stubs, not real services).
- **Integration tests (testcontainers)** — `internal/postgres/*_integration_test.go`
  and `internal/auth/integration_test.go` run against real Postgres 16 and
  Redis 7 containers started per test by `testcontainers-go` (postgres + redis
  modules). Migrations run for real in these tests.
- **Pipeline E2E** (`test/e2e`, issue #38) — the full movie flow: search →
  parse → quality/profile match → send to a **real download client** (the real
  qBittorrent / SABnzbd adapters built through the `downloads.NewClient`
  factory) → import, against real Postgres persistence and a real Redis pub/sub
  queue bus; the remote torrent/NZB servers are stubbed in-process over HTTP,
  so no external client is required.
- **Load test** (`test/loadtest`, issue #45) — mirrors the production wiring in
  `cmd/mediarr` (real Postgres + Redis via testcontainers, the real API server
  and middleware, all four pipelines with the fake indexer + mock client).
  Four phases: seed **10,000 items** (4k movies, 3k series, 2k artists, 1k
  authors), API latency (200 GETs/read endpoint at concurrency 10), sustained
  import (50 back-to-back movie pipelines, first-half vs second-half rate
  ratio), and peak RSS. Thresholds (dashboard p95 ≤ 250 ms, list p95 ≤ 250 ms,
  import ≥ 15 items/min, sustainability ratio ≥ 0.50, peak RSS ≤ 600 MiB) are
  enforced in the harness. Run with `make loadtest`; documented in
  `docs/loadtest.md`.
- **UI tests** — `ui/`: `npm run build` (tsc type-check + Vite build),
  `npm run lint`, and `npm run test` (vitest).
- **CI gates** (`.github/workflows/ci.yml`; triggers: PRs to `main`, pushes to
  `main`, `v*` tags, and `workflow_dispatch` with a `dry_run` input that does
  the full build/scan/notes without publishing — the documented #48 dry run):
  - `ui` — `npm ci`, type-check + build, lint, vitest (Node 22).
  - `lint-and-test` — `gofmt` check, `go vet`, **`govulncheck`**, then all
    unit tests across `internal/` with the PLAN §10 **60% overall coverage
    floor** enforced (per-package 80% target for `domains/` + `services/` is
    reported, not yet a hard gate); then the testcontainers integration tests
    and the E2E pipeline suite.
  - `semgrep` — static analysis with pinned `semgrep==1.181.0`, rulesets
    `p/golang` + `p/typescript` over the whole repo (Go + `ui/` TS/TSX),
    `--error` so any finding fails the build (the M7 #49 gate).
  - `load` — the 10k-item load harness (`go test ./test/loadtest/...`),
    on every PR and main push.
  - `docker` — local build of `deploy/Dockerfile` (ldflags-stamped),
    **Trivy** image scan (fail on `CRITICAL,HIGH`, `ignore-unfixed`),
    **SBOM** (Trivy SPDX, uploaded as an artifact); on `main` and `v*` tags
    only: multi-arch (`amd64`, `arm64`) publish to
    `ghcr.io/brandenk514/mediarr` + **cosign** (v3.1.3) signing with the
    repo's long-lived release key (public key `docs/cosign.pub`, private key
    in the `COSIGN_KEY`/`COSIGN_PASSWORD` repo secrets — the account has no
    Actions OIDC, so keyless/Fulcio signing is not available; see
    `RELEASE.md` §Signing key) and immediate `cosign verify --key
    docs/cosign.pub`; tag pushes additionally promote the tag to `:latest`
    via `buildx imagetools`.
  - `smoke` (pushes only) — `docker compose -f deploy/compose.dev.yml up
    -d --build`, then probe `/healthz`, `/readyz`, `/version` until ready,
    then the served-UI probes (`scripts/ui-smoke.sh`: SPA root, client-route
    deep link, hashed asset, reserved API routes), then tear down.
  - `ui-e2e` (pull_request only) — builds the PR-local image (single-arch,
    loaded into the daemon), serves it on an isolated ephemeral compose stack
    (`deploy/compose.ci.yml`, project `mediarr-ci`), seeds the first-boot
    admin, runs the served-UI probes, then the Playwright browser subset
    (`ui/e2e/container-ui.spec.ts`: real login form → dashboard renders →
    each per-kind page loads without unhandled JS errors). The full 23-spec
    suite in `ui/e2e/` is the local bar: `cd ui && npm run e2e` against the
    compose dev stack (`deploy/compose.dev.yml` + Vite).
  - `release-notes` (tag pushes only) — `scripts/release-notes.sh` renders
    notes from the issues referenced by the tag's commits and creates the
    GitHub release (`gh release create`).

## 13. Documentation map

| Doc | What it covers |
|---|---|
| `docs/PLAN.md` | Original design intent, milestones M0–M7, open items |
| `docs/architecture.md` | This document — as-built architecture |
| `docs/config.md` | Full environment-variable reference + defaults |
| `docs/observability.md` | Metrics reference, Prometheus/Grafana wiring |
| `docs/loadtest.md` | Load-test methodology and thresholds |
| `RELEASE.md` (repo root) | Release process: tags, signing, deployment |
| `deploy/helm/README.md` | Helm chart values and security model |
