import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { Episode, ListQuery, PageSeries, Series } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import {
  AzJump,
  EmptyState,
  Menu,
  Toolbar,
  TbButton,
  ViewToggle,
  type ViewMode,
} from '../../components/ui'

// TvPage is the Sonarr-parity TV library (P2): a 6-column poster grid by
// default (poster with an optional red status ribbon over a light footer with
// 'Monitored' + quality profile), an A–Z quick-jump on the right edge, and a
// Sonarr-style toolbar (icon-over-label groups with dividers). The list view
// is the P1 expandable table, kept behind the View toggle. Sorting is
// client-side over the loaded page; filtering (monitored) stays server-side.
type SortKey = 'title-asc' | 'title-desc' | 'year-asc' | 'year-desc' | 'added-asc' | 'added-desc'

const SORT_OPTIONS = [
  { value: 'title-asc', label: 'Title ↑' },
  { value: 'title-desc', label: 'Title ↓' },
  { value: 'year-asc', label: 'Year ↑' },
  { value: 'year-desc', label: 'Year ↓' },
  { value: 'added-asc', label: 'Added ↑' },
  { value: 'added-desc', label: 'Added ↓' },
] as const

function sortSeries(items: Series[], key: SortKey): Series[] {
  const dir = key.endsWith('-desc') ? -1 : 1
  const arr = [...items]
  arr.sort((a, b) => {
    let cmp = 0
    if (key.startsWith('title')) cmp = a.Title.localeCompare(b.Title)
    else if (key.startsWith('year')) cmp = a.Year - b.Year
    else cmp = String(a.AddedAt).localeCompare(String(b.AddedAt))
    if (cmp === 0) cmp = a.ID - b.ID // stable tiebreak
    return cmp * dir
  })
  return arr
}

// posterText derives a lightweight poster stand-in (two-word initials) since
// poster art is not served yet.
function posterText(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '🎬'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}

function firstLetter(title: string): string {
  // Skip leading articles ("The Wire" -> W, Sonarr-style).
  const words = title.trim().split(/\s+/)
  for (const w of words) {
    if (!/^(the|a|an)$/i.test(w) && /^[a-z]/i.test(w)) return w[0]!.toUpperCase()
  }
  const c = title.trim()[0]
  return c ? c.toUpperCase() : '#'
}

export function TvPage() {
  const navigate = useNavigate()
  const load = useCallback((q: ListQuery) => client.listSeries(q), [])
  const {
    data,
    loading,
    error,
    monitored,
    setMonitored,
    page,
    setPage,
    total,
    reload,
    expanded,
    toggleExpanded,
  } = usePagedList<PageSeries>(load)

  const [view, setView] = useState<ViewMode>('grid')
  const [sort, setSort] = useState<SortKey>('title-asc')
  const [status, setStatus] = useState<string | null>(null)

  const items = useMemo(() => (data ? sortSeries(data.items, sort) : []), [data, sort])
  const azLetters = useMemo(() => [...new Set(items.map((s) => firstLetter(s.Title)))].sort(), [items])

  function jumpTo(letter: string) {
    const el = document.querySelector(`[data-series-letter="${letter}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }

  function updateAll() {
    setStatus(`Updated ${total} series just now`)
    reload()
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="tv-title">Series</h2>
        <div className="dash-head-right">
          <AddButton to="/tv/add" label="Add series" testId="tv-add" />
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="tv-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="tv-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Series"
              testId="tv-select-series"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
            <TbButton
              icon="🔍"
              label="Test Parsing"
              testId="tv-test-parsing"
              disabled
              title="Parse testing lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="tv-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <ViewToggle view={view} onChange={setView} testId="tv-view" />
            <Menu
              label="Sort"
              icon="↕"
              options={[...SORT_OPTIONS]}
              selected={sort}
              onSelect={(v) => setSort(v as SortKey)}
              testId="tv-sort"
            />
            <Menu
              label="Filter"
              icon="⚲"
              options={[
                { value: 'all', label: 'All' },
                { value: 'monitored', label: 'Monitored' },
                { value: 'unmonitored', label: 'Unmonitored' },
              ]}
              selected={monitored}
              onSelect={(v) => setMonitored(v as 'all' | 'monitored' | 'unmonitored')}
              testId="tv-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="tv-status" role="status">
          {status}
        </p>
      )}

      {error && !data ? (
        <div className="card" data-testid="tv-error">
          <h3>TV unavailable</h3>
          <p className="error" data-testid="tv-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                {loading && !data ? (
                  <p className="dim">Loading series…</p>
                ) : items.length === 0 ? (
                  <EmptyState title="No series" message="Add your first series to get started.">
                    <Link to="/tv/add" data-testid="tv-empty-add">
                      + Add series
                    </Link>
                  </EmptyState>
                ) : (
                  <div className="poster-grid" data-testid="tv-grid">
                    {items.map((s) => (
                      <PosterCard key={s.ID} s={s} onOpen={() => navigate(`/tv/${s.ID}`)} />
                    ))}
                  </div>
                )}
              </div>
              {items.length > 0 && <AzJump available={azLetters} onJump={jumpTo} testId="tv-az" />}
            </div>
          )}

          {view === 'list' && (
            <div className="list-list" data-testid="tv-list">
              {loading && !data ? (
                <p className="dim">Loading series…</p>
              ) : items.length === 0 ? (
                <p className="dim" data-testid="tv-empty">
                  No series match.
                </p>
              ) : (
                items.map((s) => (
                  <div className="list-item" key={s.ID} data-testid={`series-${s.ID}`}>
                    <div className="item-head">
                      <button
                        type="button"
                        className={`row-head ${expanded.has(s.ID) ? 'open' : ''}`}
                        onClick={() => toggleExpanded(s.ID)}
                        aria-expanded={expanded.has(s.ID)}
                        data-testid={`series-toggle-${s.ID}`}
                      >
                        <span className={`chevron ${expanded.has(s.ID) ? 'chevron-open' : ''}`}>
                          {expanded.has(s.ID) ? '▾' : '▸'}
                        </span>
                        <span className="lt-name" title={s.Title}>
                          {s.Title}
                        </span>
                        {s.Year ? <span className="lt-dim">{s.Year}</span> : null}
                      </button>
                      <MonitoredToggle
                        monitored={s.Monitored}
                        onToggle={async (next) => {
                          await client.setSeriesMonitored(s.ID, next)
                          reload()
                        }}
                        label={`${s.Title} monitoring`}
                        testId={`series-mon-${s.ID}`}
                      />
                    </div>
                    {expanded.has(s.ID) && <EpisodeList seriesId={s.ID} />}
                  </div>
                ))
              )}
            </div>
          )}

          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </>
      )}
    </div>
  )
}

// PosterCard is one grid cell: 2:3 poster (with a red status ribbon when the
// series is unmonitored) over the light two-line footer. Clicking the card
// opens the series detail page.
function PosterCard({ s, onOpen }: { s: Series; onOpen: () => void }) {
  return (
    <button
      type="button"
      className="poster-card"
      onClick={onOpen}
      data-testid={`series-card-${s.ID}`}
      data-series-letter={firstLetter(s.Title)}
      title={`${s.Title}${s.Year ? ` (${s.Year})` : ''}`}
    >
      {!s.Monitored && <span className="ribbon" data-testid={`ribbon-${s.ID}`}>Unmonitored</span>}
      <span className="poster" aria-hidden>
        {posterText(s.Title)}
      </span>
      <span className="poster-foot">
        <span className="pf-mon">{s.Monitored ? 'Monitored' : 'Unmonitored'}</span>
        <span className="pf-q" data-testid={`card-quality-${s.ID}`}>
          {s.QualityProfile || '—'}
        </span>
      </span>
    </button>
  )
}

// EpisodeList fetches a series' episodes only after its row is expanded (list
// view), so collapsed rows cost no child request.
function EpisodeList({ seriesId }: { seriesId: number }) {
  const [eps, setEps] = useState<Episode[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .listEpisodes(seriesId)
      .then((e) => !cancelled && setEps(e ?? [])) // older backends return null
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load episodes'))
    return () => {
      cancelled = true
    }
  }, [seriesId])

  if (err) return <p className="error">Episode error: {err}</p>
  if (!eps) return <p className="dim">Loading episodes…</p>
  if (eps.length === 0) return <p className="dim">No episodes yet.</p>
  return (
    <div className="child-list" data-testid={`episodes-${seriesId}`}>
      {eps.map((ep) => (
        <div className="child-row" key={ep.ID}>
          <span className="child-label">
            S{ep.Season}E{ep.Episode}
          </span>
          <span className="lt-name" title={ep.Title}>
            {ep.Title || '—'}
          </span>
          <MonitoredToggle
            monitored={ep.Monitored}
            onToggle={async (next) => {
              await client.setEpisodeMonitored(seriesId, ep.Season, ep.Episode, next)
            }}
            label={`S${ep.Season}E${ep.Episode} monitoring`}
            testId={`episode-mon-${ep.ID}`}
          />
        </div>
      ))}
    </div>
  )
}
