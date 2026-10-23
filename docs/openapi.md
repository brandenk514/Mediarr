# OpenAPI contract

`openapi.yaml` is the **source of truth for the API wire format** — every
`/api/v1` route (70 operations, 51 paths), plus the ops routes
(`/healthz`, `/readyz`, `/version`) and the `/api/v1/events` WebSocket.
The Go handlers and the UI types are both downstream of it.

## Wire-format conventions

- **Library items** (movies, series, episodes, artists, albums, tracks,
  authors, titles, editions) serialize with their **exact Go field names**
  (`ID`, `Title`, `Monitored`, `AddedAt`) — the domain structs carry no
  json tags, so the spec must mirror the bare names. Do **not** "fix"
  these to camelCase.
- **API-only documents** (list envelopes, request bodies, indexers,
  settings) use snake_case tags (`items`, `page_size`, `release_title`).
- **Nullability is a contract, not an accident:** `Page.items` and the
  dashboard's arrays are always `[]`, never `null` (a `null` here crashes
  the UI). Child collections (episodes, albums, tracks, titles, editions,
  wanted lists) and queue-history event lists are `null` when empty —
  the repos return nil slices — so those fields are `nullable: true`.
  If you change a nil-slice to an empty slice (or vice versa), change the
  spec in the same commit.

## Contributor workflow

Change Go → update spec → regenerate → PR:

1. Make the API change in `internal/api/`.
2. Update `openapi.yaml` to match (new route, param, status code, or
   body shape — all of it).
3. `make spec-check` — structural validation (kin-openapi); CI runs the
   same check on every PR.
4. `make generate` — regenerates `ui/src/api/types.gen.ts`
   (openapi-typescript). Commit the regenerated file.
5. The e2e drift test (`test/e2e/openapi_drift_test.go`) validates every
   operation against a live server — request and response — in the normal
   e2e run; a mismatch fails the build.

## Lockstep gates

| Gate | Where | Catches |
| --- | --- | --- |
| `make spec-check` | CI "Lint, vet, test" job + local | Malformed spec |
| Regenerate + `git diff --exit-code` | CI "UI — build, lint, test" job | Stale `types.gen.ts` (fails with the exact fix command) |
| `TestOpenAPIDrift` | e2e target (Postgres + Redis harness) | Spec-vs-server drift on all 70 ops + WebSocket frames |

The CI regenerate is byte-for-byte: `openapi-typescript` output is
deterministic for a given spec + pinned toolchain (7.13.0 in the
lockfile), so a local `make generate` and the CI regeneration agree.

## Consumers

- **UI**: `ui/src/api/types.gen.ts` (generated; never hand-edit) is
  re-exported by `ui/src/api/types.ts` as the module the app imports.
- **Backend**: `tools/speccheck` (the `make spec-check` loader) and the
  drift test both load the spec with kin-openapi.
- **Future clients**: generate from the same spec; the nullability
  contract above is what a client may rely on.
