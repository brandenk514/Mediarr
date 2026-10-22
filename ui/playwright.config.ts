import { defineConfig } from '@playwright/test'

// Browser E2E for the mediarr UI.
//
// These specs run against a *running* stack: the compose dev backend on
// :8080 (postgres + redis inside the compose network) and Vite on :5173.
// CI webServer wiring (fresh stack + seeded user) is a follow-up; set
// E2E_BASE_URL to point at another instance.
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
