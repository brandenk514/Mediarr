import type { Page } from '@playwright/test'

// Credentials for the disposable e2e probe user (see the seeded row in
// `users`). Overridable via env so CI can inject its own.
export const E2E_USER = process.env.E2E_USER ?? 'e2e_probe'
export const E2E_PASSWORD = process.env.E2E_PASSWORD ?? 'playwright-e2e-probe-9f2c'

// signIn drives the real login form. The caller must already be on /login:
// the token only lives in page memory (ui/src/api/store.ts), so a full page
// load after sign-in would drop it — navigate client-side (or let the app's
// returnTo in Login.tsx do it), never page.goto, once authenticated.
export async function signIn(page: Page, password: string = E2E_PASSWORD): Promise<void> {
  await page.getByTestId('username').fill(E2E_USER)
  await page.getByTestId('password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
}
