import { useEffect, useState } from 'react'
import { ApiError, client } from '../../../api/client'
import type {
  MediaKind,
  MetadataSourceSearchResult,
  MetadataSourceSettings,
  ProviderAttempt,
} from '../../../api/types'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// MetadataSourceSection is the P5.7 section of the P5 settings shell: the
// ordered provider chain per library kind (keyed primary first, keyless
// fallbacks after). Unconfigured (key-less) sources are skipped by the
// server's fallback chain, so a zero-key install still searches through
// TVmaze / Wikidata / MusicBrainz / Open Library. The test-search box runs
// the saved chain live (503 = every source failed; the attempt log shows).
const KINDS: MediaKind[] = ['movie', 'tv', 'music', 'book']
const PROVIDERS = ['tmdb', 'tvdb', 'tvmaze', 'wikidata', 'musicbrainz', 'openlibrary'] as const
const KEYED = new Set<string>(['tmdb', 'tvdb'])

type ChainOutcome = { result: MetadataSourceSearchResult; allFailed: boolean }

export function MetadataSourceSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<MetadataSourceSettings>(
    'metadata-source',
  )
  // Local, editable per-kind chains: kind -> ordered provider list.
  const [sources, setSources] = useState<Record<string, string[]>>({})
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  // Chain test state.
  const [qKind, setQKind] = useState<MediaKind>('tv')
  const [query, setQuery] = useState('')
  const [chain, setChain] = useState<ChainOutcome | null>(null)
  const [chainErr, setChainErr] = useState('')
  const [searching, setSearching] = useState(false)

  useEffect(() => {
    if (doc) {
      const next: Record<string, string[]> = {}
      for (const k of KINDS) next[k] = doc.sources?.[k] ?? []
      setSources(next)
    }
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="metadata-source-error" />
  }

  function move(kind: string, i: number, dir: -1 | 1) {
    setSources((s) => {
      const list = [...(s[kind] ?? [])]
      const j = i + dir
      if (j < 0 || j >= list.length) return s
      ;[list[i], list[j]] = [list[j], list[i]]
      return { ...s, [kind]: list }
    })
  }

  function addProvider(kind: string, provider: string) {
    if (!provider) return
    setSources((s) => (s[kind]?.includes(provider) ? s : { ...s, [kind]: [...(s[kind] ?? []), provider] }))
  }

  function removeProvider(kind: string, i: number) {
    setSources((s) => ({ ...s, [kind]: (s[kind] ?? []).filter((_, j) => j !== i) }))
  }

  function handleSave() {
    void submit(async () => {
      const next: MetadataSourceSettings = {
        sources: Object.fromEntries(KINDS.map((k) => [k, sources[k] ?? []])),
      }
      await client.putSettingsSection<MetadataSourceSettings>('metadata-source', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  async function runChain() {
    if (searching) return
    setSearching(true)
    setChainErr('')
    setChain(null)
    try {
      const res = await client.searchMetadataSource({ kind: qKind, query })
      setChain({ result: res, allFailed: res.source === '' })
    } catch (e) {
      if (e instanceof ApiError) {
        // 503 with the attempt log in the body: show the failed chain.
        try {
          const parsed = JSON.parse(e.body) as MetadataSourceSearchResult
          setChain({ result: parsed, allFailed: true })
          return
        } catch {
          // not a result body — fall through to the plain error
        }
        setChainErr(e.message)
      } else {
        setChainErr('search failed')
      }
    } finally {
      setSearching(false)
    }
  }

  return (
    <>
      <SectionCard
        title="Metadata sources (P5.7)"
        hint="The ordered provider chain per library kind. First working source wins; unconfigured (key-less) sources are skipped, so keyless installs still work."
        testId="ms-card"
      >
        {KINDS.map((k) => (
          <div className="subform" key={k} data-testid={`ms-kind-${k}`}>
            <h4>{k}</h4>
            {(sources[k] ?? []).map((p, i) => (
              <div className="ms-row" key={`${p}-${i}`} data-testid={`ms-src-${k}-${i}`}>
                <span className="pill">
                  {p}
                  {KEYED.has(p) ? ' (keyed)' : ' (keyless)'}
                </span>
                <button
                  className="ghost"
                  disabled={i === 0}
                  onClick={() => move(k, i, -1)}
                  data-testid={`ms-up-${k}-${i}`}
                >
                  ↑
                </button>
                <button
                  className="ghost"
                  disabled={i === (sources[k] ?? []).length - 1}
                  onClick={() => move(k, i, 1)}
                  data-testid={`ms-down-${k}-${i}`}
                >
                  ↓
                </button>
                <button className="ghost" onClick={() => removeProvider(k, i)} data-testid={`ms-del-${k}-${i}`}>
                  ×
                </button>
              </div>
            ))}
            {(sources[k] ?? []).length === 0 && (
              <span className="dim small">no sources (a kind needs at least one to save)</span>
            )}
            <select value="" onChange={(e) => addProvider(k, e.target.value)} data-testid={`ms-add-${k}`}>
              <option value="">Add source…</option>
              {PROVIDERS.filter((p) => !(sources[k] ?? []).includes(p)).map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
        ))}
        {saved && (
          <p className="pill pill-ok" data-testid="ms-saved">
            Saved
          </p>
        )}
        <SaveError error={saveError} testId="ms-error-save" />
        <button onClick={handleSave} disabled={busy || loading} data-testid="ms-save">
          {busy ? 'Saving…' : 'Save metadata sources'}
        </button>
      </SectionCard>

      <SectionCard
        title="Test the chain"
        hint="Runs a live search through the saved chain (unsaved edits are not included)."
        testId="ms-search-card"
      >
        <select value={qKind} onChange={(e) => setQKind(e.target.value as MediaKind)} data-testid="ms-search-kind">
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        <label htmlFor="ms-search-query">Query</label>
        <input id="ms-search-query" value={query} onChange={(e) => setQuery(e.target.value)} data-testid="ms-search-query" />
        <button onClick={runChain} disabled={searching || query.trim() === ''} data-testid="ms-search-run">
          {searching ? 'Searching…' : 'Run search'}
        </button>
        {chainErr && (
          <p className="error" data-testid="ms-search-error">
            {chainErr}
          </p>
        )}
        {chain && <ChainResult r={chain.result} allFailed={chain.allFailed} />}
      </SectionCard>
    </>
  )
}

function ChainResult({ r, allFailed }: { r: MetadataSourceSearchResult; allFailed: boolean }) {
  return (
    <div data-testid="ms-chain-result">
      {allFailed ? (
        <p className="error" data-testid="ms-chain-allfailed">
          Every configured source failed for {r.kind}.
        </p>
      ) : (
        <p data-testid="ms-chain-winner">
          answered by <strong data-testid="ms-chain-source">{r.source}</strong> — {r.results.length} result
          {r.results.length === 1 ? '' : 's'}
        </p>
      )}
      {r.results.slice(0, 5).map((res, i) => (
        <p className="dim small" key={`${res.title}-${i}`} data-testid={`ms-chain-hit-${i}`}>
          {res.title}
          {res.year ? ` (${res.year})` : ''}
        </p>
      ))}
      {r.attempts.length > 0 && (
        <div data-testid="ms-chain-attempts">
          {r.attempts.map((a: ProviderAttempt) => (
            <span
              key={a.source}
              className={a.ok ? 'pill pill-ok' : 'pill'}
              data-testid={`ms-attempt-${a.source}`}
            >
              {a.source}: {a.ok ? `ok ${a.latency_ms}ms` : a.error || 'failed'}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
