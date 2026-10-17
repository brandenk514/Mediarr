# Observability (M7 #46)

Mediarr exposes a Prometheus endpoint and ships ready-made scrape config,
alert rules, a Grafana dashboard, and an optional dev compose add-on.

## Endpoint

```
GET /metrics            # Bearer token required, like the rest of the API
```

The token comes from `POST /api/v1/auth/login` (single-user v1). In the dev
stack:

```sh
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' -d @login.json | jq -r .token)
echo "$TOKEN" > deploy/prometheus/mediarr-token
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/metrics | head
```

## Metrics

All metrics live under the `mediarr_` prefix. Every label is bounded by
design so series counts stay flat as the library grows:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `mediarr_build_info` | gauge | `version`, `commit`, `build_time` | 1 while up; stamped at build time |
| `mediarr_http_requests_total` | counter | `method`, `status` | HTTP requests by final status |
| `mediarr_http_request_duration_seconds` | histogram | `method`, `route` | Latency per registered route pattern |
| `mediarr_queue_depth` | gauge | `kind`, `state` | Items per queue state, counted at scrape time from Postgres |
| `mediarr_pipeline_runs_total` | counter | `kind`, `result` | Pipeline runs; `result ∈ {ok, error, skip}` |
| `mediarr_pipeline_run_duration_seconds` | histogram | `kind` | End-to-end pipeline latency |
| `mediarr_pipeline_imports_total` | counter | `kind` | Import work items completed (only on ok runs) |
| `mediarr_indexer_calls_total` | counter | `name`, `result` | Indexer calls; `result ∈ {success, failure}` |
| `mediarr_indexer_call_latency_seconds` | histogram | `name` | Indexer call latency |
| `mediarr_indexer_healthy` | gauge | `name` | 1 if the last call succeeded, 0 otherwise |
| `mediarr_library_items` | gauge | `kind` | Rows in each library table, counted at scrape time |
| `mediarr_dependency_up` | gauge | `name` | Postgres/Redis liveness, re-checked at scrape time |

Label vocabulary:

- `kind ∈ {movie, tv, music, book}`
- `state ∈ {queued, downloading, imported, failed}`
- `result` (pipeline) `∈ {ok, error, skip}`; (indexer) `∈ {success, failure}`
- `name` = configured indexer name
- `route` = registered mux pattern (e.g. `GET /api/v1/movies/{id}`), never a
  raw path with IDs. The `/api/v1/events` WebSocket route is not instrumented
  (a multi-hour socket would poison the latency histogram).

`go_goroutines`, `go_gc_duration_seconds`, and friends come from
`prometheus/collectors` defaults, so the runtime is always visible.

## Running it

**Dev stack add-on** (Prometheus + Grafana in the same compose project):

```sh
docker compose -f deploy/compose.dev.yml -f deploy/compose.observability.yml up --build
```

- Prometheus → http://localhost:9090
- Grafana → http://localhost:3000 (admin / admin)
  1. Data sources → Add Prometheus → `http://prometheus:9090`
  2. Dashboards → Import → `deploy/grafana/mediarr-dashboard.json`

**Standalone** (see the header of `deploy/prometheus/prometheus.yml` for the
`docker run` one-liner).

## Alerting

`deploy/prometheus/rules.yml` defines five rules:

| Alert | Condition |
| --- | --- |
| `MediarrQueueStuck` | Queue non-empty for 30m |
| `MediarrQueueFailing` | >3 failed entries in 30m |
| `MediarrPipelineFailing` | >50% of pipeline runs failing for 15m |
| `MediarrIndexerDown` | Indexer's last call failing for 15m |
| `MediarrDependencyDown` | Postgres or Redis down for 5m |

Wire them to your notifier of choice (PagerDuty, Discord, …) via a
`alerting:` block in `prometheus.yml` — the rules themselves stay in this
repo.

## Load-test thresholds (M7 #45)

The load test's pass/fail thresholds are expressed against these metrics:
queue depth, pipeline run rate by outcome, import throughput
(`mediarr_pipeline_imports_total`), and HTTP latency percentiles
(`mediarr_http_request_duration_seconds`). See `docs/PLAN.md` → M7.
