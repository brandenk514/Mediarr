// In-memory bearer-token holder (issue #39).
//
// Acceptance: "no secrets in the SPA (token in memory, not localStorage)".
// The token lives only in a module-level variable for the lifetime of the page
// session. It is never written to localStorage/sessionStorage/cookies, so a
// full page refresh drops it and the user is asked to log in again. This is a
// deliberate v1 trade-off (no silent re-auth) that keeps the token out of
// persistent browser storage.
let token: string | undefined

export function getToken(): string | undefined {
  return token
}

export function setToken(t: string): void {
  token = t
}

export function clearToken(): void {
  token = undefined
}
