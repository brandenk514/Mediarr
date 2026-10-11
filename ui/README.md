# mediarr UI

React + TypeScript SPA for mediarr (M6). See `docs/PLAN.md` §12 — the UI is a
separate build; the Go service is the source of truth for all data.

## Stack

- Vite + React 19 + TypeScript (strict)
- `react-router-dom` for routing
- `vitest` + `@testing-library/react` for tests

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

## Development

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
