# mediarr UI

React + TypeScript SPA for mediarr (M6). See `docs/PLAN.md` §12 — the UI is
a separate build; the Go service is the source of truth for all data.

## Stack

- Vite + React 19 + TypeScript (strict)
- `react-router-dom` for routing
- `vitest` + `@testing-library/react` for tests
- Playwright (`ui/e2e/`) for browser E2E against a running stack

## Container mode vs dev mode

- **Container mode (what ships).** The UI is *built inside the image* by the
  `ui-build` stage of `deploy/Dockerfile` (Node 22: `npm ci` from the
  committed lockfile, then `npm run build`) and the finished `dist` lands at
  `/home/nonroot/ui`. The Go server serves it same-origin on `:8080` with
  SPA fallback (#87; see `docs/architecture.md` §11). Running the app needs
  **no Node at all** — `npm ci`/`npm run build` only ever run in the build
  stage or in CI. `MEDIARR_UI_DIR` points the server at the asset root
  (default `/home/nonroot/ui`; a missing/empty root 404s UI paths only —
  API and health routes are unaffected).
- **Dev mode (source work).** The Vite dev server runs the UI from source on
  `:5173` and proxies `/api` to the backend. This is for UI development
  only — the container path above does not use it.

## Layout

```
src/
  api/       typed v1 API client (client.ts), wire types (types.ts),
             in-memory token store (store.ts)
  auth/      AuthProvider + useAuth (token in memory, never browser storage)
  pages/     Layout (nav shell), LoginPage, per-kind views (M6 #40–#44)
  test/      vitest setup
```

## Auth model (issue #39)

Login is `POST /api/v1/auth/login` (first boot: the entered username +
password become the initial admin). The returned bearer token is kept in a
module-level variable (`src/api/store.ts`) for the page session only —
never written to `localStorage`/`sessionStorage`/cookies. A refresh drops the
token and the user signs in again; that is a deliberate v1 trade-off.

## Development (dev mode)

```sh
npm install          # add --include=dev if NODE_ENV=production is set
npm run dev          # Vite dev server on :5173, proxies /api → :8080
```

Point the proxy at another instance with `MEDIARR_API_URL=http://host:port`.

Against the real service, start it first:

```sh
docker compose -f ../deploy/compose.dev.yml up
```

## Scripts

| command           | what                          |
| ----------------- | ----------------------------- |
| `npm run dev`     | Vite dev server + proxy       |
| `npm run build`   | `tsc -b` type-check + build   |
| `npm run lint`    | oxlint                        |
| `npm test`        | vitest (unit/component)       |

CI: the `ui` job in `.github/workflows/ci.yml` runs install → build → lint →
test on every PR (separate from the Go pipeline).
