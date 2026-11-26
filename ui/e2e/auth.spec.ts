import { expect, test } from '@playwright/test'
import { signIn } from './login'

// 401s map to this exact message in ui/src/api/client.ts (ApiError).
const INVALID_CREDENTIALS_TEXT = 'authentication expired or invalid'

test('unauthenticated access to an app page redirects to /login', async ({ page }) => {
  await page.goto('/movies')
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByTestId('username')).toBeVisible()
})

test('wrong password surfaces the 401 error and stays on /login', async ({ page }) => {
  await page.goto('/login')
  await signIn(page, 'definitely-not-the-right-password')
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByTestId('login-error')).toHaveText(INVALID_CREDENTIALS_TEXT)
})

test('valid credentials sign in and land on the unified Discover page', async ({ page }) => {
  // v0.4.0 shell (#118): the default landing is the unified cross-kind
  // Discover page (supersedes the R7 Movies-list landing) — a sign-in with
  // no return target lands on /, which redirects to /discover.
  await page.goto('/login')
  await signIn(page)
  await expect(page).toHaveURL(/\/discover$/)
  await expect(page.getByTestId('discover-page-title')).toBeVisible()
  // The login form is gone; we are behind the gate.
  await expect(page.getByTestId('username')).not.toBeVisible()
})
