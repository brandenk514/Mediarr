import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { ListQuery, MusicLibrarySummary, PageArtist } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { MUSIC_RIBBON_LEGEND, deriveMusicRibbon, humanBytes } from '../../components/music'
import { MusicGrid, fromArtist } from '../../components/musicgrid'
import {
  EmptyState,
  Menu,
  Toolbar,
  TbButton,
  ViewToggle,
  type ViewMode,
} from '../../components/ui'

// MusicPage is the Lidarr L1 artists library: a 6-up square-cover card grid
// with the five-state status ribbon (Downloading / Continuing / Ended /
// Missing Tracks ×2), a red corner fold on unmonitored (flagged) artists,
// the library footer legend, and the library summary status bar (Artist ·
// Inactive · Continuing · Monitored · Unmonitored · Tracks · Files · Total
// File Size from GET /music/library/summary). The toolbar follows Lidarr's
// order: Update All, RSS Sync | Select Artists | Options | View · Sort ·
// Filter — buttons without a backend behind them are disabled stubs with a
// "later phase" title (the P7 pattern); Update All re-fetches the library.
// The A–Z quick jump and the list view carry over from the prior table.
type SortKey = 'name-asc' | 'name-desc' | 'added-asc' | 'added-desc'

const SORT_OPTIONS = [
  { value: 'name-asc', label: 'Name ↑' },
  { value: 'name-desc', label: 'Name ↓' },
  { value: 'added-asc', label: 'Added ↑' },
  { value: 'added-desc', label: 'Added ↓' },
] as const

// statusFilterValue: the toolbar Filter menu's status-group options (the
// five ribbon states). 'all' / 'monitored' / 'unmonitored' are server-side.
type FilterValue = 'all' | 'monitored' | 'unmonitored' | 'downloading' | 'continuing' | 'ended' | 'missing-monitored' | 'missing-unmonitored'

function sortArtists(items: PageArtist['items'], key: SortKey): PageArtist['items'] {
  const dir = key.endsWith('-desc') ? -1 : 1
  const arr = [...items]
  arr.sort((a, b) => {
    let cmp = 0
    if (key.startsWith('name')) cmp = a.Name.localeCompare(b.Name)
    else cmp = String(a.AddedAt).localeCompare(String(b.AddedAt))
    if (cmp === 0) cmp = a.ID - b.ID // stable tiebreak
    return cmp * dir
  })
  return arr
}

export function MusicPage() {
  const navigate = useNavigate()
  const load = useCallback((q: ListQuery) => client.listArtists(q), [])
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
  } = usePagedList<PageArtist>(load)

  const [view, setView] = useState<ViewMode>('grid')
  const [sort, setSort] = useState<SortKey>('name-asc')
  const [statusFilter, setStatusFilter] = useState<Exclude<FilterValue, 'all' | 'monitored' | 'unmonitored'> | null>(null)
  const [summary, setSummary] = useState<MusicLibrarySummary | null>(null)
  const [status, setStatus] = useState<string | null>(null)

  // Library summary for the footer status bar (Lidarr-specific; no Sonarr
  // equivalent). Cosmetic — a failure must not break the grid.
  const loadSummary = useCallback(() => {
    client
      .getMusicLibrarySummary()
      .then(setSummary)
      .catch(() => setSummary(null))
  }, [])

  useEffect(() => {
    loadSummary()
  }, [loadSummary, data])

  const baseItems = useMemo(() => (data ? data.items : []), [data])

  const items = useMemo(() => {
    const filtered = statusFilter
      ? baseItems.filter((a) => deriveMusicRibbon(a).state === statusFilter)
      : baseItems
    return sortArtists(filtered, sort)
  }, [baseItems, statusFilter, sort])

  // Live counts for the Filter menu's status-group options (over the
  // server-filtered page, before the status filter is applied).
  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = {
      downloading: 0,
      continuing: 0,
      ended: 0,
      'missing-monitored': 0,
      'missing-unmonitored': 0,
    }
    for (const a of baseItems) counts[deriveMusicRibbon(a).state]++
    return counts
  }, [baseItems])

  function updateAll() {
    setStatus(`Updated ${total} artists just now`)
    reload()
    loadSummary()
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="music-title">Artists</h2>
        <div className="dash-head-right">
          <AddButton to="/music/add" label="Add artist" testId="music-add" />
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="music-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="music-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Artists"
              testId="music-select"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="music-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <ViewToggle view={view} onChange={setView} testId="music-view" />
            <Menu
              label="Sort"
              icon="↕"
              options={[...SORT_OPTIONS]}
              selected={sort}
              onSelect={(v) => setSort(v as SortKey)}
              testId="music-sort"
            />
            <Menu
              label="Filter"
              icon="⚲"
              options={[
                { value: 'all', label: 'All' },
                { value: 'monitored', label: 'Monitored' },
                { value: 'unmonitored', label: 'Unmonitored' },
                ...MUSIC_RIBBON_LEGEND.map((l) => ({
                  value: l.state,
                  label: `${l.label} (${statusCounts[l.state] ?? 0})`,
                })),
              ]}
              selected={statusFilter ?? monitored}
              onSelect={(v) => {
                if (v === 'monitored' || v === 'unmonitored' || v === 'all') {
                  setStatusFilter(null)
                  setMonitored(v as 'all' | 'monitored' | 'unmonitored')
                } else {
                  setStatusFilter(v as Exclude<FilterValue, 'all' | 'monitored' | 'unmonitored'>)
                }
              }}
              testId="music-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="music-status" role="status">
          {status}
        </p>
      )}

      {error && !data ? (
        <div className="card" data-testid="music-error">
          <h3>Artists unavailable</h3>
          <p className="error" data-testid="music-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !data ? (
                <p className="dim" data-testid="music-loading">
                  Loading artists…
                </p>
              ) : items.length === 0 ? (
                <EmptyState
                  title={statusFilter ? 'No artists in this state' : 'No artists'}
                  message={
                    statusFilter
                      ? 'Try a different filter.'
                      : 'Add your first artist to get started.'
                  }
                >
                  {!statusFilter && (
                    <Link to="/music/add" data-testid="music-empty-add">
                      + Add artist
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <MusicGrid
                  items={items.map(fromArtist)}
                  gridTestId="music-grid"
                  azTestId="music-az"
                  onOpen={(i) => navigate(`/music/${i.id}`)}
                />
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="music-table">
              <div className="lt-head">
                <span>Artist</span>
                <span>Monitor</span>
                <span>Quality</span>
                <span>Status</span>
                <span>Tracks</span>
                <span>Files</span>
                <span>Size</span>
              </div>
              {loading && !data ? (
                <p className="dim">Loading artists…</p>
              ) : items.length === 0 ? (
                <p className="dim" data-testid="music-empty">
                  No artists match.
                </p>
              ) : (
                items.map((a) => (
                  <div className="lt-row" key={a.ID} data-testid={`artist-${a.ID}`}>
                    <span className="lt-name" title={a.Name}>
                      <Link to={`/music/${a.ID}`} data-testid={`artist-link-${a.ID}`}>
                        {a.Name}
                      </Link>
                    </span>
                    <span>
                      <MonitoredToggle
                        monitored={a.Monitored}
                        onToggle={async (next) => {
                          await client.setArtistMonitored(a.ID, next)
                          reload()
                        }}
                        label={`${a.Name} monitoring`}
                        testId={`artist-mon-${a.ID}`}
                      />
                    </span>
                    <span className="lt-dim">{a.QualityProfile || '—'}</span>
                    <span className="lt-dim">{deriveMusicRibbon(a).state.replace('-', ' ')}</span>
                    <span className="lt-dim">{a.TrackCount ?? 0}</span>
                    <span className="lt-dim">{a.FileCount ?? 0}</span>
                    <span className="lt-dim">
                      {a.TotalSize ? humanBytes(a.TotalSize) : '—'}
                    </span>
                  </div>
                ))
              )}
            </div>
          )}

          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />

          {/* Library footer: summary status bar + ribbon legend (Lidarr's
              "library footer"). */}
          {summary && (
            <div className="stats-bar" data-testid="music-summary">
              <span>
                Artist <b data-testid="summary-artists">{summary.artists}</b>
              </span>
              <span>
                Inactive <b data-testid="summary-inactive">{summary.inactive}</b>
              </span>
              <span>
                Continuing <b data-testid="summary-continuing">{summary.continuing}</b>
              </span>
              <span>
                Monitored <b data-testid="summary-monitored">{summary.monitored}</b>
              </span>
              <span>
                Unmonitored <b data-testid="summary-unmonitored">{summary.unmonitored}</b>
              </span>
              <span>
                Tracks <b data-testid="summary-tracks">{summary.tracks}</b>
              </span>
              <span>
                Files <b data-testid="summary-files">{summary.files}</b>
              </span>
              <span>
                Total File Size <b data-testid="summary-size">{humanBytes(summary.total_file_size)}</b>
              </span>
            </div>
          )}

          <div className="ribbon-legend" data-testid="music-legend">
            {MUSIC_RIBBON_LEGEND.map((l) => (
              <span key={l.state} data-testid={`legend-${l.state}`}>
                <span className={`legend-swatch ${l.tone}`} aria-hidden />
                {l.label}
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
