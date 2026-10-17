# Load test (M7 #45)

A scripted, reproducible load harness that proves Mediarr behaves at scale
target: **10k items in the DB** with sustained import pressure.

- **Harness:** `test/loadtest/load_test.go` (package `loadtest_test`)
- **Run:** `make loadtest` — or `go test ./test/loadtest/... -count=1 -timeout 600s -v` (requires Docker for testcontainers)
- **CI:** the `Load test (10k items)` job in `.github/workflows/ci.yml` runs it on every PR and on main.

## What it does

The harness mirrors the production wiring in `cmd/mediarr` — real Postgres +
Redis via testcontainers, the real API server and middleware, all four
pipelines (fake indexer + mock download client, the same in-process model the
e2e/integration suites established) — and runs four phases:

1. **Seed 10,000 items** via bulk INSERT: 4,000 movies, 3,000 series,
   2,000 artists, 1,000 authors (10k items total).
2. **API latency:** 200 GETs per read endpoint at concurrency 10 against the
   full HTTP server (dashboard, paginated movie/tv/music/books lists, queue)
   → p50/p95/p99 per endpoint.
3. **Sustained import:** 50 full movie pipelines back to back
   (add → indexer search → profile match → mock download → import), reporting
   items/min and a **first-half vs second-half rate ratio** — import must stay
   *sustained*, not just fast at the start.
4. **Memory:** peak process RSS sampled at 2 Hz during phases 2–3.

## Thresholds

| # | Threshold | Value | Rationale |
|---|-----------|-------|-----------|
| 1 | Dashboard p95 | ≤ 250 ms | Dashboard is the heaviest read (cross-library snapshot: counts + queue depth + dependency liveness re-ping). 250 ms keeps the UI's refresh loop comfortably interactive on a LAN; above it the dashboard would feel broken. |
| 2 | List p95 | ≤ 250 ms | Single-table `COUNT(*) + LIMIT/OFFSET` at 10k rows; ~8–30 ms on dev hardware, 100–220 ms on 2-core CI runners. 250 ms is a regression gate (10× headroom over baseline), not a pinned target — a real UI-feel target belongs in a UI test, not a CI load job. |
| 3 | Import rate | ≥ 15 items/min | With the **mock** download client (no network I/O) this measures DB writes + file I/O + pipeline bookkeeping — the floor a real client would sit *above*. 15 items/min ≈ one import per 4 s: a real torrent arriving every few seconds is handled without queue buildup. |
| 4 | Sustainability ratio | ≥ 0.50 | The second half of the import run must hold ≥ 50% of the first half's rate. A ratio < 1 means import degrades over time — the signature of unbounded growth (query plans, queue depth, allocations). 0.50 (not tighter) because per-item runtimes are ~2 ms with the mock client, so 25-item halves carry large timing variance on slower CI machines; a run that halves its throughput is still an unambiguous red flag. |
| 5 | Peak RSS | ≤ 600 MiB | See the caveat below: this is a test-*process* ceiling, not a single-container ceiling. 600 MiB (vs a ~40–80 MiB idle baseline) bounds the working set of the whole app under the 10k-scale + load phases, and stays far below the 1 GiB dev compose memory limit. |

## Baseline

First full green run of the harness (verified twice; numbers below are from
the second run):

- Date: 2026-10-17
- Environment: Intel Core i7-12700H (20 threads), 32 GiB RAM, Linux; Go
  1.27.2 in `golang:1.27.2-bookworm`; Postgres 16 + Redis 7 via
  testcontainers (Docker); single namespace, single node.
- Results:

  | Phase | Result | Threshold |
  |---|---|---|
  | Seed | 10,000 items in **46 ms** | (informational) |
  | Dashboard p95 (n=200) | **8.5 ms** (p50 2.5 ms, p99 14.4 ms) | ≤ 250 ms |
  | Movies p95 (n=200) | **29.1 ms** (p50 24.6 ms, p99 30.8 ms) | ≤ 250 ms |
  | Queue p95 (n=200) | **5.0 ms** (p50 0.9 ms, p99 7.9 ms) | ≤ 250 ms |
  | TV p95 (n=200) | **23.7 ms** (p50 20.0 ms, p99 25.9 ms) | ≤ 250 ms |
  | Music p95 (n=200) | **15.5 ms** (p50 12.1 ms, p99 17.2 ms) | ≤ 250 ms |
  | Books p95 (n=200) | **8.5 ms** (p50 6.5 ms, p99 12.1 ms) | ≤ 250 ms |
  | Sustained import | 50 items in **75 ms** ≈ **40,000 items/min** (mock client) | ≥ 15 items/min |
  | Sustainability ratio (2nd half ÷ 1st half) | **1.53** | ≥ 0.50 |
  | Peak RSS | **39 MiB** | ≤ 600 MiB |

  All thresholds pass with wide headroom; the first run (53 ms seed, p95
  8.3/29.1/4.1/24.3/15.4/8.5 ms, ratio 0.71) matched within run-to-run noise.

## Caveats

- **In-process RSS.** The app under test runs inside the test binary (the
  established model of this repo's e2e/integration suites), so the RSS number
  includes the Go runtime and test scaffolding. The distroless image adds only
  a few MB on top; the threshold carries headroom for that.
- **Mock download client.** The sustained-import phase uses the mock client —
  no real download I/O. Throughput with a real client (qBittorrent/SABnzbd)
  is bounded by the client and network, not by Mediarr; the mock measures the
  server-side floor.
- **Single node.** No sharding/replication scenario — Mediarr is a single
  Postgres-backed service by design; scale-out is a network concern, not an
  application one.
