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

test('valid credentials sign in and land on the dashboard', async ({ page }) => {
  await page.goto('/login')
  await signIn(page)
  await expect(page).toHaveURL(/\/$/)
  // The login form is gone; we are behind the gate.
  await expect(page.getByTestId('username')).not.toBeVisible()
})
