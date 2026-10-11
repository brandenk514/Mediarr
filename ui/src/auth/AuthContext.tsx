import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { client } from '../api/client'
import { getToken } from '../api/store'
import type { Me } from '../api/types'

interface AuthContextValue {
  me: Me | null
  /** True once a token exists in memory and is being validated (or was). */
  ready: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [ready, setReady] = useState(false)

  // On mount, if a token already lives in memory (e.g. a client-side navigation
  // after login), validate it once. A 401 drops the token and shows login again.
  useEffect(() => {
    if (!getToken()) {
      setReady(true)
      return
    }
    client
      .me()
      .then((u) => setMe(u))
      .catch(() => {
        client.logout()
        setMe(null)
      })
      .finally(() => setReady(true))
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    await client.login({ username, password })
    setMe(await client.me())
  }, [])

  const logout = useCallback(() => {
    client.logout()
    setMe(null)
  }, [])

  const value = useMemo(() => ({ me, ready, login, logout }), [me, ready, login, logout])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
