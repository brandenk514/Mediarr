// Shared building blocks for the four add flows (#42): provider search with
// debounce → result cards → quality-profile picker → add. Each kind page
// (pages/add/*) composes these with its own search function and add request.
//
//   - SearchBox: debounced provider search box with loading/empty/error
//     states. A 503 from the API means the provider isn't configured; that is
//     surfaced inline and the box is disabled (no retry on it — a config
//     change requires a server restart).
//   - ResultList: the hit cards; selecting one closes the search phase.
//   - ProfilePicker: built-in quality profiles for a kind, prefilled with
//     the kind's server default.
//   - useProfiles: fetches the profile list + default once.
//   - useDebouncedSearch: debounce + stale-response guard shared by all four
//     flows.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client } from '../api/client'
import type { ProfileKind, ProfilesResponse, SearchResult } from '../api/types'

export const SEARCH_DEBOUNCE_MS = 350
// Minimum characters before a provider search fires: a single keystroke to
// TMDB/TVDB is wasted bandwidth (and rate-limited) and almost never useful.
export const MIN_SEARCH_CHARS = 2

type Phase = 'idle' | 'loading' | 'results' | 'error' | 'unavailable'

export interface SearchState {
  phase: Phase
  hits: SearchResult[]
  error: string
}

// AddButton is the list-view header button that jumps to the kind's add
// flow (one per kind page).
export function AddButton({ to, label, testId }: { to: string; label: string; testId: string }) {
  return (
    <Link className="ghost" to={to} data-testid={testId}>
      + {label}
    </Link>
  )
}

// useDebouncedSearch runs a debounced provider search whenever `term`
// (trimmed) crosses MIN_SEARCH_CHARS. Stale responses are dropped so a slow
// answer to "du" can never overwrite the results of "dune".
export function useDebouncedSearch(search: (q: string) => Promise<SearchResult[]>) {
  const [text, setText] = useState('')
  const [state, setState] = useState<SearchState>({
    phase: 'idle',
    hits: [],
    error: '',
  })
  const reqId = useRef(0)

  useEffect(() => {
    const term = text.trim()
    if (term.length < MIN_SEARCH_CHARS) {
      // Reset when the query is too short — but keep any 503 note visible.
      reqId.current += 1 // invalidate in-flight
      setState((s) =>
        s.phase === 'unavailable'
          ? s
          : { phase: 'idle', hits: [], error: '' },
      )
      return
    }
    const timer = setTimeout(async () => {
      const id = ++reqId.current
      setState((s) => ({
        ...s,
        phase: 'loading',
        error: '',
      }))
      try {
        const hits = await search(term)
        if (id !== reqId.current) return
        setState({ phase: 'results', hits, error: '' })
      } catch (e: unknown) {
        if (id !== reqId.current) return
        if (e instanceof ApiError && e.status === 503) {
          // Provider not configured: grey out the box, no retry offered.
          setState({ phase: 'unavailable', hits: [], error: e.message })
        } else {
          setState({
            phase: 'error',
            hits: [],
            error: e instanceof Error ? e.message : 'search failed',
          })
        }
      }
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [text, search])

  return { text, setText, state }
}

// SearchBox is the provider search input. It shows a spinner while a search
// is in flight, the 503 reason (disabled box) when the provider is not
// configured, and a retry-able error line for transient failures.
export function SearchBox({
  label,
  placeholder,
  text,
  onText,
  state,
  testId,
}: {
  label: string
  placeholder: string
  text: string
  onText: (s: string) => void
  state: SearchState
  testId: string
}) {
  const { phase } = state
  return (
    <div className="search-block" data-testid={testId}>
      <label htmlFor={`${testId}-input`}>
        {label}
        {phase === 'loading' && <span className="dim" data-testid={`${testId}-loading`}> searching…</span>}
      </label>
      <div className="search-row">
        <input
          id={`${testId}-input`}
          className="list-search"
          type="search"
          placeholder={placeholder}
          value={text}
          onChange={(e) => onText(e.target.value)}
          disabled={phase === 'unavailable'}
          data-testid={`${testId}-input`}
        />
      </div>
      {phase === 'unavailable' && (
        <p className="dim" data-testid={`${testId}-unavailable`}>
          {state.error}
        </p>
      )}
      {phase === 'error' && (
        <p className="error" data-testid={`${testId}-error`}>
          {state.error}
        </p>
      )}
    </div>
  )
}

// ResultList renders the provider hits as selectable cards. A card shows the
// hit's display detail from `extra` (overview/rating/country/author per
// kind) and the year when known. Clicking selects the hit and closes the
// search phase.
export function ResultList({
  hits,
  onPick,
  testId,
}: {
  hits: SearchResult[]
  onPick: (hit: SearchResult) => void
  testId: string
}) {
  if (hits.length === 0) {
    return (
      <p className="dim" data-testid={`${testId}-empty`}>
        No results — try a different term.
      </p>
    )
  }
  return (
    <ul className="result-list" data-testid={testId}>
      {hits.map((h) => (
        <li key={h.id}>
          <button
            type="button"
            className="result-card"
            onClick={() => onPick(h)}
            data-testid={`result-${h.id}`}
          >
            <span className="result-title">
              {h.title}
              {h.year ? <span className="dim"> ({h.year})</span> : null}
            </span>
            {renderDetail(h)}
          </button>
        </li>
      ))}
    </ul>
  )
}

// renderDetail pulls the kind-specific display fields out of a hit's extra
// map. Movies/TV carry overview + rating; artists carry country; book works
// carry the author.
function renderDetail(h: SearchResult) {
  const { extra } = h
  const bits: string[] = []
  if (extra.rating && extra.rating !== '0.0') bits.push(`★ ${extra.rating}`)
  if (extra.country) bits.push(extra.country)
  if (extra.author) bits.push(extra.author)
  const detail = [...(extra.overview ? [extra.overview] : []), ...bits].join(' — ')
  if (!detail) return null
  return <span className="dim small">{detail}</span>
}

// useProfiles fetches the built-in profiles + default for a kind once.
export function useProfiles(kind: ProfileKind) {
  const [data, setData] = useState<ProfilesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .getProfiles(kind)
      .then((p) => !cancelled && setData(p))
      .catch((e: unknown) =>
        !cancelled && setError(e instanceof Error ? e.message : 'failed to load profiles'),
      )
    return () => {
      cancelled = true
    }
  }, [kind])

  return { data, error }
}

// ProfilePicker is the quality-profile dropdown. It preselects the kind's
// server default (from GET /api/v1/profiles) when the page hasn't set a
// value yet, then mirrors the page's state.
export function ProfilePicker({
  kind,
  value,
  onChange,
  testId,
}: {
  kind: ProfileKind
  value: string
  onChange: (name: string) => void
  testId: string
}) {
  const { data, error } = useProfiles(kind)

  // Preselect the default once the list arrives (only while unchosen).
  useEffect(() => {
    if (data && value === '' && data.default) onChange(data.default)
  }, [data, value, onChange])

  if (error) {
    return (
      <div className="search-block">
        <label>Quality profile</label>
        <p className="error" data-testid={`${testId}-error`}>
          {error}
        </p>
      </div>
    )
  }
  if (!data) {
    return (
      <div className="search-block">
        <label>Quality profile</label>
        <p className="dim" data-testid={`${testId}-loading`}>
          Loading profiles…
        </p>
      </div>
    )
  }
  return (
    <div className="search-block">
      <label htmlFor={testId}>Quality profile</label>
      <select
        id={testId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        data-testid={testId}
      >
        {data.profiles.map((p) => (
          <option key={p} value={p}>
            {p}
            {p === data.default ? ' (default)' : ''}
          </option>
        ))}
      </select>
    </div>
  )
}

// MonitoredField is the add-flow "monitor this item?" checkbox. It is the
// one optional field every add request accepts; unchecked sends an explicit
// false (the API defaults to monitored when the field is omitted).
export function MonitoredField({
  label,
  checked,
  onChecked,
  testId,
}: {
  label: string
  checked: boolean
  onChecked: (v: boolean) => void
  testId: string
}) {
  return (
    <label className="check" htmlFor={testId}>
      <input
        id={testId}
        type="checkbox"
        checked={checked}
        onChange={(e) => onChecked(e.target.checked)}
        data-testid={testId}
      />
      {label}
    </label>
  )
}

// AddError renders the API's inline error after an add attempt (400
// validation, 409 conflict, etc.). The add endpoint is idempotent, so a 409
// from these endpoints would only occur for a genuine server-side conflict —
// the message from the API is shown verbatim.
export function AddError({ message, testId }: { message: string; testId: string }) {
  if (!message) return null
  return (
    <p className="error" role="alert" data-testid={testId}>
      {message}
    </p>
  )
}

// useAddSubmit wraps one add call with busy/error state. `run` should return
// the promise for the client.addX(...) call; on success the add-flow page
// navigates to the library page.
export function useAddSubmit<T>(run: () => Promise<T>) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = useCallback(async () => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await run()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'failed to add')
      throw e
    } finally {
      setBusy(false)
    }
  }, [busy, run])

  return { busy, error, submit }
}
