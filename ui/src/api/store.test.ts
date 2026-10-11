import { describe, expect, it } from 'vitest'
import { clearToken, getToken, setToken } from './store'

// The token store is the #39 "no secrets in the SPA" boundary: the token must
// live in memory only, never in localStorage/sessionStorage/cookies.
describe('in-memory token store', () => {
  it('stores and returns the token', () => {
    setToken('ma_123')
    expect(getToken()).toBe('ma_123')
  })

  it('clears on logout', () => {
    setToken('ma_123')
    clearToken()
    expect(getToken()).toBeUndefined()
  })

  it('never persists to browser storage', () => {
    setToken('ma_persist-me')
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
    expect(document.cookie).toBe('')
  })
})
