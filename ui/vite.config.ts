/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { configDefaults } from 'vitest/config'
import { defineConfig } from 'vite'

// https://vite.dev/config/
//
// The SPA is built as a separate artifact (PLAN §12, `ui/` "separate build").
// In dev, Vite serves the app and proxies /api to the mediarr service, so the
// UI client can use relative URLs with no CORS setup.
export default defineConfig({
  plugins: [react()],
  server: {
    // The dev server runs with --host so it is reachable from other machines
    // (e.g. http://ai-local-llm.local.uaccloud.com:5173). Vite 8 otherwise
    // rejects requests whose Host header it doesn't recognize.
    allowedHosts: true,
    proxy: {
      '/api': {
        target: process.env.MEDIARR_API_URL ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // The host sets NODE_ENV=production globally; that makes React load its
    // production build, which lacks React.act (testing-library needs it).
    // Pin the test env regardless of the surrounding shell.
    env: { NODE_ENV: 'test' },
    // Playwright e2e specs live in e2e/ and run via `npm run e2e`
    // (see playwright.config.ts); keep vitest on unit/component tests only.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
