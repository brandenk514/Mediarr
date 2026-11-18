import { defineConfig } from '@playwright/test'

// Browser E2E for the mediarr UI.
//
// These specs run against a *running* stack: the compose dev backend on
// :8080 (postgres + redis inside the compose network) and Vite on :5173.
// Set E2E_BASE_URL to point at another instance.
//
// CI webServer wiring (issue #90, this follow-up): instead of a `webServer`
// entry that starts a dev server, the `ui-e2e` job in
// .github/workflows/ci.yml starts an ISOLATED, ephemeral compose stack
// (deploy/compose.ci.yml, project `mediarr-ci`) serving the PR-local image,
// seeds the first-boot admin, and runs these specs with E2E_BASE_URL pointed
// at :8080. The stack is external to Playwright (it outlives the browser
// process and holds the seeded user), so no in-config webServer is needed —
// the job owns the stack's lifecycle (up, seed, down -v on success AND
// failure). The disposable first-boot-admin / E2E_USER+E2E_PASSWORD pattern
// is documented in deploy/compose.ci.yml and ui/e2e/login.ts.
export default defineConfig({
  testDir: './e2e',
  timeout: 20_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
    headless: true,
    trace: 'retain-on-failure',
  },
})
