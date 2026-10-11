/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
//
// The SPA is built as a separate artifact (PLAN §12, `ui/` "separate build").
// In dev, Vite serves the app and proxies /api to the mediarr service, so the
// UI client can use relative URLs with no CORS setup.
export default defineConfig({
  plugins: [react()],
  server: {
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
  },
})
