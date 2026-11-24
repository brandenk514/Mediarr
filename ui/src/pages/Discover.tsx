// Discover — the unified cross-kind Discover page (issue #118, final
// slice). The app's default landing: three feed rows (Trending,
// Upcoming, New Releases), each a grid of kind-badged PosterCards (the
// #115 design-system primitives, as proven in GlobalSearch) grouped by
// kind (movie / tv / music / book — the server's deterministic per-row
// order, the server's (row × kind) 6-item cap).
//
// Pure UI over the finished GET /api/v1/discover contract (A1+A2): the
// response carries the three rows plus `unavailable` (per-cell failures).
// A cell can appear in BOTH a row and `unavailable` (the keyless Google
// Books case: Open Library fallback items AND the missing key named) —
// the row renders its items plus the grey-out note; an unavailable entry
// never hides a row. Only a TOTAL request failure shows a page-level
// error; per-cell problems stay per-cell notes (the R3 keyless pattern).
//
// Navigation (no invented routes):
//   - upcoming rows carry LOCAL library ids: movie / tv / music cards
//     deep-link to the kind's detail route (/movies/:id, /tv/:id,
//     /music/:id); book items are title ids and there is no title-level
//     detail route (the /books/:id deep route is the AUTHOR page), so
//     they render without an onOpen.
//   - trending / new releases rows carry EXTERNAL ids (TMDB /
//     MusicBrainz / Open Library / Google Books). Only the Movies add
//     flow accepts an external id (the R3 ?tmdb=<numeric> preselect),
//     so TMDB movie feed cards link to /movies/add?tmdb=…; every other
//     kind's add flow is a manual search (no query params) → no link.

import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { client } from '../api/client'
import type { DiscoverCellError, DiscoverItem, DiscoverPage } from '../api/types'
import { PosterCard } from '../components/primitives'
import { EmptyState } from '../components/ui'

// ---- row / kind model -------------------------------------------------------

// The three rows, in render order. `cellRow` is the row key the server
// uses in the `unavailable` cells ("<kind>:<row>").
export const DISCOVER_ROWS = [
  {
    id: 'trending',
    label: 'Trending',
    cellRow: 'trending',
    empty: 'Nothing trending right now.',
    emptyMessage: 'What’s hot across movies, TV, music, and books shows up here.',
  },
  {
    id: 'upcoming',
    label: 'Upcoming',
    cellRow: 'upcoming',
    empty: 'Nothing coming up.',
    emptyMessage: 'Monitored air and release dates in the next 14 days appear here.',
  },
  {
    id: 'newReleases',
    label: 'New Releases',
    cellRow: 'new_releases',
    empty: 'No new releases right now.',
    emptyMessage: 'The freshest releases from the feeds appear here.',
  },
] as const
export type DiscoverRowId = (typeof DISCOVER_ROWS)[number]['id']
export type DiscoverRow = (typeof DISCOVER_ROWS)[number]

// The deterministic per-row kind order (movie, tv, music, book) — the
// same order the server emits each row's cells in.
export const DISCOVER_KINDS = ['movie', 'tv', 'music', 'book'] as const
export type DiscoverKind = (typeof DISCOVER_KINDS)[number]

// Human kind labels (the GlobalSearch convention: the singular "TV", the
// plural "Movies / Music / Books").
export const DISCOVER_KIND_LABEL: Record<DiscoverKind, string> = {
  movie: 'Movies',
  tv: 'TV',
  music: 'Music',
  book: 'Books',
}

// ---- pure helpers (unit-testable) -------------------------------------------

// groupRowItems splits a row's items into per-kind groups in the
// deterministic kind order, dropping kinds with no items. The server
// already emits each row in this order, but the UI groups defensively
// (the cap is per (row × kind), not per row).
export function groupRowItems(items: DiscoverItem[]): [DiscoverKind, DiscoverItem[]][] {
  const groups: [DiscoverKind, DiscoverItem[]][] = []
  for (const kind of DISCOVER_KINDS) {
    const cell = items.filter((i) => i.kind === kind)
    if (cell.length > 0) groups.push([kind, cell])
  }
  return groups
}

// cellErrorsForRow picks the unavailable cells belonging to one row (the
// server's "<kind>:<row>" naming), keeping the server's cell order
// (per row: movie, tv, music, book).
export function cellErrorsForRow(row: DiscoverRow, cells: DiscoverCellError[]): DiscoverCellError[] {
  return cells.filter((c) => {
    const [kind, r] = c.cell.split(':')
    return r === row.cellRow && (DISCOVER_KINDS as readonly string[]).includes(kind)
  })
}

// itemMeta composes the card's second line from the item's one-line
// subtitle (artist / author / studio / episode SxE) and, when known, the
// year. Every field may be missing (year 0, subtitle '') — the join
// keeps no stray separators. For items that carry a date (upcoming
// rows) the human date leads; the bare year is then redundant and is
// left out.
export function itemMeta(item: DiscoverItem): string {
  const parts: string[] = []
  const date = dateLabel(item.date)
  if (date) parts.push(date)
  if (item.subtitle) parts.push(item.subtitle)
  if (!date && item.year > 0) parts.push(String(item.year))
  return parts.join(' · ')
}

// dateLabel is the human date for an RFC3339 instant (the upcoming rows'
// air/release starts); '' when the item carries none (partial dates) or
// the value is not a parseable date (the UI must never crash on either).
export function dateLabel(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

// targetFor picks a card's navigation target, or null (the card renders
// without an onOpen — the PosterCard button stays inert):
//   upcoming rows (local library ids): movie / tv / music → the kind's
//     detail route; book items are title ids with no title-level detail
//     route → no link.
//   feed rows (external ids): only the Movies add flow accepts an
//     external id (the R3 ?tmdb=<numeric> preselect, numeric-validated
//     like MovieAddPage does) → link; every other kind's add flow is a
//     manual search (no query params) → no link.
export function targetFor(row: DiscoverRow, item: DiscoverItem): string | null {
  if (row.id === 'upcoming') {
    switch (item.kind) {
      case 'movie':
        return `/movies/${item.sourceId}`
      case 'tv':
        return `/tv/${item.sourceId}`
      case 'music':
        return `/music/${item.sourceId}`
      default:
        return null
    }
  }
  if (item.kind === 'movie' && /^\d+$/.test(item.sourceId)) {
    return `/movies/add?tmdb=${item.sourceId}`
  }
  return null
}

// ---- page --------------------------------------------------------------------

export function DiscoverPage() {
  const navigate = useNavigate()
  const [page, setPage] = useState<DiscoverPage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    client
      .discover()
      .then((r) => setPage(r))
      .catch((e: unknown) => {
        // Per-cell failures never land here: the server folds them into
        // `unavailable` and answers 200. This path is a total failure
        // (network, auth) — a page-level error with a retry.
        setError(e instanceof Error ? e.message : 'failed to load discover')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  if (error) {
    return (
      <div className="list-page" data-testid="discover-page">
        <div className="page-head">
          <h2 data-testid="discover-page-title">Discover</h2>
        </div>
        <div className="card" data-testid="discover-error">
          <p className="error" data-testid="discover-error-message">
            {error}
          </p>
          <button type="button" data-testid="discover-retry" onClick={load}>
            Retry
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="list-page" data-testid="discover-page">
      <div className="page-head">
        <h2 data-testid="discover-page-title">Discover</h2>
      </div>
      {DISCOVER_ROWS.map((row) => (
        <DiscoverRowView
          key={row.id}
          row={row}
          loading={loading}
          items={page?.[row.id] ?? []}
          unavailable={page ? cellErrorsForRow(row, page.unavailable) : []}
          onOpen={(to) => navigate(to)}
        />
      ))}
    </div>
  )
}

// DiscoverRowView renders one row: the row title, then — in the page's
// single loading phase — a dim loading line (the established loading
// pattern: Calendar, R3 Discover, Global Search). Once loaded: the
// per-kind grids (when the row has items), the per-cell grey-out notes
// (when cells are unavailable), or — a row with zero items AND no
// unavailable cells — the EmptyState. A zero-item row whose cells are
// unavailable shows the notes alone: they ARE the row's content (the
// feed is unconfigured), and an EmptyState would read as "empty" when
// the real answer is "missing key".
function DiscoverRowView({
  row,
  loading,
  items,
  unavailable,
  onOpen,
}: {
  row: DiscoverRow
  loading: boolean
  items: DiscoverItem[]
  unavailable: DiscoverCellError[]
  onOpen: (to: string) => void
}) {
  const groups = groupRowItems(items)
  return (
    <section className="discover-row" data-testid={`discover-row-${row.id}`}>
      <h3 className="discover-row-title" data-testid={`discover-row-title-${row.id}`}>
        {row.label}
      </h3>
      {loading ? (
        <p className="dim" data-testid={`discover-row-loading-${row.id}`}>
          Loading {row.label.toLowerCase()}…
        </p>
      ) : groups.length === 0 && unavailable.length === 0 ? (
        <EmptyState
          testId={`discover-row-empty-${row.id}`}
          title={row.empty}
          message={row.emptyMessage}
        />
      ) : (
        <>
          {groups.length > 0 && (
            <div className="poster-grid" data-testid={`discover-row-grid-${row.id}`}>
              {groups.map(([kind, cellItems]) =>
                cellItems.map((item, i) => (
                  <DiscoverCard
                    key={`${kind}-${item.sourceId}-${i}`}
                    item={item}
                    kind={kind}
                    row={row}
                    onOpen={onOpen}
                  />
                )),
              )}
            </div>
          )}
          {unavailable.map((c) => (
            <UnavailableNote key={c.cell} cell={c} />
          ))}
        </>
      )}
    </section>
  )
}

// UnavailableNote is one cell's grey-out note (the R3 keyless pattern):
// it names the feed (the kind label) plus the server's reason, and it
// never hides the row's items — a cell can carry BOTH items and a note
// (the keyless Google Books fallback case).
function UnavailableNote({ cell }: { cell: DiscoverCellError }) {
  const [kind] = cell.cell.split(':')
  const label = DISCOVER_KIND_LABEL[kind as DiscoverKind] ?? kind
  return (
    <p className="dim small" data-testid={`discover-unavailable-${cell.cell.replace(':', '-')}`}>
      {label}: {cell.reason}
    </p>
  )
}

// DiscoverCard is one poster cell: the #115 PosterCard whose status
// badge over the art is the KIND badge (the GlobalSearch pattern), the
// poster or the initials stand-in (posterUrl '' → undefined →
// posterInitials), the composed meta line, and — where targetFor found
// one — a navigation target.
function DiscoverCard({
  item,
  kind,
  row,
  onOpen,
}: {
  item: DiscoverItem
  kind: DiscoverKind
  row: DiscoverRow
  onOpen: (to: string) => void
}) {
  const to = targetFor(row, item)
  return (
    <PosterCard
      title={item.title}
      image={item.posterUrl || undefined}
      meta={itemMeta(item) || undefined}
      status={{ label: DISCOVER_KIND_LABEL[kind], tone: 'accent' }}
      onOpen={to ? () => onOpen(to) : undefined}
      testId={`discover-card-${row.id}-${kind}-${item.sourceId}`}
    />
  )
}
