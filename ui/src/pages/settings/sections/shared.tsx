import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client } from '../../../api/client'
import type { SettingsSection } from '../../../api/types'

// shared.tsx holds the small building blocks every P5 settings section
// composes: the per-section document loader (defaults-merged read + error
// states), the busy/error submit wrapper, the error pill, and the section
// card frame.

// useSubmit is a busy/error wrapper around an async action (mirrors the
// add-flow useAddSubmit but does not re-throw — settings save errors are shown
// inline, not surfaced to a caller).
export function useSubmit() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = useCallback(
    async (run: () => Promise<void>) => {
      if (busy) return
      setBusy(true)
      setError('')
      try {
        await run()
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : 'save failed')
      } finally {
        setBusy(false)
      }
    },
    [busy],
  )

  return { busy, error, submit }
}

export function SaveError({ error, testId }: { error: string; testId: string }) {
  if (!error) return null
  return (
    <p className="error" role="alert" data-testid={testId}>
      {error}
    </p>
  )
}

// useSectionDoc loads one generic settings section document. The server merges
// stored values over the section defaults, so `doc` is complete as soon as it
// arrives (a fresh install reads default documents). Load failures surface as
// `error` (+ `authLost` on 401) rather than blanking the section.
export function useSectionDoc<T>(section: SettingsSection) {
  const [doc, setDoc] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    setAuthLost(false)
    client
      .getSettingsSection<T>(section)
      .then((d) => {
        if (!cancelled) setDoc(d)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 401) setAuthLost(true)
        setError(e instanceof Error ? e.message : 'failed to load settings')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [section, refreshKey])

  return { doc, loading, error, authLost, setDoc, refresh: () => setRefreshKey((k) => k + 1) }
}

// SectionError is the per-section error gate (the section's own load failed —
// the shell's nav still works, so a section failure never blanks the page).
export function SectionError({
  message,
  authLost,
  onRetry,
  testId,
}: {
  message: string
  authLost: boolean
  onRetry: () => void
  testId: string
}) {
  return (
    <div className="card" data-testid={testId}>
      <h3>Section unavailable</h3>
      <p className="error" data-testid={`${testId}-message`}>
        {message}
      </p>
      {authLost && (
        <p>
          <Link to="/login">Sign in again</Link>
        </p>
      )}
      <button onClick={onRetry} data-testid={`${testId}-retry`}>
        Retry
      </button>
    </div>
  )
}

// SectionCard is the frame every section renders its content in.
export function SectionCard({
  title,
  hint,
  testId,
  children,
}: {
  title: string
  hint?: string
  testId?: string
  children: React.ReactNode
}) {
  return (
    <div className="card" data-testid={testId}>
      <h3>{title}</h3>
      {hint && <p className="dim small">{hint}</p>}
      {children}
    </div>
  )
}
