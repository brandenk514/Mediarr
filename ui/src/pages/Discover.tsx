// Discover — the Seerr-style cinematic row set (issue #151; absorbs the
// retired Dashboard, issue #148). The app's default landing:
//
//   1. a compact per-kind summary strip (the Dashboard KPI counts),
//   2. a recent-activity landscape row (the Dashboard's recent queue +
//      recent history feeds),
//   3. the discover feed rows (Trending mixed, Popular Movies / Popular
//      Series, Upcoming Movies / Upcoming Series — the mapped reference
//      inventory; the recorded deviations: no Series Genres / Networks
//      rows, no dedicated music/book feed rows),
//   4. Your Watchlist (the monitored library),
//   5. the Movie Genres / Studios facet rows (inert tiles — see
//      components/discover/facet-tile).
//
// Every row is a chevron-scroller carousel (components/discover/row.tsx):
// horizontal scroll is the responsive mechanism at 375/768px (#123) and
// the track is keyboard scrollable.
//
// Degradation (acceptance-critical): NO raw provider string (e.g.
// 'tmdb is not configured (missing API key)') is ever rendered. The one
// deterministic mapper in pages/discover/model.ts (cellNote / rowNotes)
// turns each DiscoverCellError into a friendly grey-out note IN the row
// (the restyled #118 pattern); a fully-keyless row shows the note in
// place of the items, and the unrendered music/book cells of the split
// rows surface their notes in one muted block at the bottom. Only a
// TOTAL discover request failure shows a page-level error with retry.
//
// Data surface (UI-only, no API changes):
//   - GET /api/v1/discover (the feed rows + per-cell `unavailable`)
//   - GET /api/v1/dashboard?recent=20 (KPI + recent activity)
//   - the four list endpoints with the #150 sort allowlist: all four
//     kinds accept "added-desc" server-side (books are a full array —
//     client-sorted by AddedAt) — Recently Added + Watchlist + facets.
//
// Navigation (no invented routes):
//   - upcoming rows carry LOCAL library ids: movie / tv / music cards
//     deep-link to the kind's detail route (/movies/:id, /tv/:id,
//     /music/:id); book items are title ids and there is no title-level
//     detail route (the /books/:id deep route is the AUTHOR page) → inert.
//   - trending / new-releases rows carry EXTERNAL ids (TMDB / MusicBrainz
//     / Open Library / Google Books). Only the Movies add flow accepts an
//     external id (the ?tmdb=<numeric> preselect) → link; every other
//     kind's add flow is a manual search (no query params) → inert.
import { useCallback, useEffect, useState } from 'react'
import { client } from '../api/client'
import type {
  Artist,
  Book,
  Dashboard,
  DiscoverPage as DiscoverFeed,
  Movie,
  Series,
} from '../api/types'
import { ActivityCardView } from '../components/discover/activity-card'
import { FacetTile } from '../components/discover/facet-tile'
import { RowNotes } from '../components/discover/note'
import { PosterCell } from '../components/discover/poster-cell'
import { DiscoverRow } from '../components/discover/row'
import { SummaryStrip } from '../components/discover/summary-strip'
import { EmptyState } from '../components/ui'
import {
  DISCOVER_ROWS,
  buildRecentlyAdded,
  buildWatchlist,
  cellsForRow,
  feedItemsForRow,
  feedItemMeta,
  feedTarget,
  mergeRecentActivity,
  rowNotes,
  topGenres,
  topStudios,
  unclaimedCells,
} from './discover/model'

// The section sizes (page 1 samples, all client-bounded): Recently Added
// asks 12 per kind (the merged row caps at 12 overall), Watchlist 50 per
// kind (the row scrolls), the facet rows sample 50 movies.
const RECENTLY_ADDED_PAGE = 12
const WATCHLIST_PAGE = 50
const FACET_PAGE = 50
const LIST_UNAVAILABLE_NOTE = 'This library could not be loaded. If it keeps happening, check Settings.'

// The page's one loading phase + the settled data.
interface Loaded {
  feed: DiscoverFeed | null
  dash: Dashboard | null
  recent: { movies: Movie[]; series: Series[]; artists: Artist[]; books: Book[]; failed: string[] }
  watch: { movies: Movie[]; series: Series[]; artists: Artist[]; books: Book[]; failed: string[] }
  facetMovies: Movie[] | null
}

export function DiscoverPage() {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoaded(null)
    setError(null)
    // The discover feed is the page: a total failure is a page-level error
    // (with retry). Everything else settles independently (Promise
    // allSettled) so one failed feed/library degrades its own section only.
    void (async () => {
      try {
        const [feedRes, dashRes, recM, recS, recA, recB, wlM, wlS, wlA, wlB, facetM] = await Promise.allSettled([
          client.discover(),
          client.getDashboard(20),
          client.listMovies({ page_size: RECENTLY_ADDED_PAGE, sort: 'added-desc' }),
          client.listSeries({ page_size: RECENTLY_ADDED_PAGE, sort: 'added-desc' }),
          client.listArtists({ page_size: RECENTLY_ADDED_PAGE, sort: 'added-desc' }),
          client.listBooks(),
          client.listMovies({ monitored: true, page_size: WATCHLIST_PAGE, sort: 'added-desc' }),
          client.listSeries({ monitored: true, page_size: WATCHLIST_PAGE, sort: 'added-desc' }),
          client.listArtists({ monitored: true, page_size: WATCHLIST_PAGE, sort: 'added-desc' }),
          client.listBooks(),
          client.listMovies({ page_size: FACET_PAGE }),
        ])
        // A missing/unreachable client method throws synchronously here — a
        // total failure for the page (the mocks in the app tests stub
        // exactly the page's calls).
        const feed = feedRes.status === 'fulfilled' ? feedRes.value : null
        if (feedRes.status === 'rejected') {
          const e = feedRes.reason
          setError(e instanceof Error ? e.message : 'failed to load discover')
          return
        }
        const recentFailed: string[] = []
        if (recM.status === 'rejected') recentFailed.push('movie')
        if (recS.status === 'rejected') recentFailed.push('tv')
        if (recA.status === 'rejected') recentFailed.push('music')
        if (recB.status === 'rejected') recentFailed.push('book')
        const watchFailed: string[] = []
        if (wlM.status === 'rejected') watchFailed.push('movie')
        if (wlS.status === 'rejected') watchFailed.push('tv')
        if (wlA.status === 'rejected') watchFailed.push('music')
        if (wlB.status === 'rejected') watchFailed.push('book')
        // Books are a full array (no pagination) — sort client-side by
        // AddedAt desc (the other three kinds come sorted "added-desc").
        const booksByAddedDesc = (r: PromiseSettledResult<Book[]>) =>
          r.status === 'fulfilled'
            ? [...r.value].sort((a, b) => Date.parse(b.AddedAt) - Date.parse(a.AddedAt))
            : []
        setLoaded({
          feed,
          dash: dashRes.status === 'fulfilled' ? dashRes.value : null,
          recent: {
            movies: recM.status === 'fulfilled' ? recM.value.items : [],
            series: recS.status === 'fulfilled' ? recS.value.items : [],
            artists: recA.status === 'fulfilled' ? recA.value.items : [],
            books: booksByAddedDesc(recB),
            failed: recentFailed,
          },
          watch: {
            movies: wlM.status === 'fulfilled' ? wlM.value.items : [],
            series: wlS.status === 'fulfilled' ? wlS.value.items : [],
            artists: wlA.status === 'fulfilled' ? wlA.value.items : [],
            books: booksByAddedDesc(wlB).filter((b) => b.Monitored),
            failed: watchFailed,
          },
          facetMovies: facetM.status === 'fulfilled' ? facetM.value.items : null,
        })
      } catch (e) {
        setError(e instanceof Error ? e.message : 'failed to load discover')
      }
    })()
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

  const feed = loaded?.feed ?? null
  const dash = loaded?.dash ?? null
  const loading = loaded === null
  const recent = loaded?.recent
  const watch = loaded?.watch
  const facetMovies = loaded?.facetMovies ?? null

  // The unrendered music/book cells of the split rows (the recorded
  // deviations) — their friendly notes surface here so no degradation
  // information is lost.
  const unclaimed = feed ? unclaimedCells(DISCOVER_ROWS, feed.unavailable) : []

  return (
    <div className="list-page" data-testid="discover-page">
      <div className="page-head">
        <h2 data-testid="discover-page-title">Discover</h2>
      </div>

      {/* 1. the compact KPI strip (the absorbed Dashboard counts). */}
      {loading ? (
        <SkeletonStrip />
      ) : dash ? (
        <SummaryStrip dash={dash} />
      ) : (
        <RowNotes notes={['The summary counts could not be loaded.']} />
      )}

      {DISCOVER_ROWS.map((row) => (
        <Row key={row.id}
          row={row}
          loading={loading}
          feed={feed}
          dash={dash}
          recent={recent}
          watch={watch}
          facetMovies={facetMovies}
        />
      ))}

      {!loading && unclaimed.length > 0 && (
        <div className="mt-4" data-testid="discover-unclaimed-notes">
          <RowNotes notes={rowNotes(unclaimed)} />
        </div>
      )}
    </div>
  )
}

// ---- one row -------------------------------------------------------------------

// Row renders a single discover row (the row chrome + its content). Each
// row id is a branch: the feed rows share the feed-row renderer; the
// library rows (recently added / watchlist) and the facet rows have their
// own. A row that has neither items nor degradation notes shows its
// EmptyState.
function Row({
  row,
  loading,
  feed,
  dash,
  recent,
  watch,
  facetMovies,
}: {
  row: (typeof DISCOVER_ROWS)[number]
  loading: boolean
  feed: DiscoverFeed | null
  dash: Dashboard | null
  recent: Loaded['recent'] | undefined
  watch: Loaded['watch'] | undefined
  facetMovies: Movie[] | null
}) {
  if (loading) {
    return (
      <DiscoverRow id={row.id} label={row.label}>
        <RowSkeleton />
      </DiscoverRow>
    )
  }
  switch (row.id) {
    case 'recentlyAdded':
      return (
        <RecentlyAddedRow row={row} recent={recent} />
      )
    case 'recentActivity':
      return <ActivityRow row={row} dash={dash} />
    case 'watchlist':
      return <WatchlistRow row={row} watch={watch} />
    case 'movieGenres':
      return <FacetRow row={row} facets={facetMovies ? topGenres(facetMovies) : []} facetFailed={facetMovies === null} />
    case 'studios':
      return <FacetRow row={row} facets={facetMovies ? topStudios(facetMovies) : []} facetFailed={facetMovies === null} />
    default:
      return <FeedRow row={row} feed={feed} />
  }
}

// FeedRow renders one of the /api/v1/discover rows: the row's kind-filtered
// items (mixed for trending), plus the friendly grey-out notes for the
// unavailable cells the row claims.
function FeedRow({ row, feed }: { row: (typeof DISCOVER_ROWS)[number]; feed: DiscoverFeed | null }) {
  const items = feed ? feedItemsForRow(row, feed) : []
  const notes = feed ? rowNotes(cellsForRow(row, feed.unavailable)) : []
  return (
    <DiscoverRow id={row.id} label={row.label}>
      {items.map((item) => (
        <PosterCell
          key={`${item.kind}-${item.sourceId}`}
          kind={item.kind}
          title={item.title}
          meta={feedItemMeta(item)}
          posterUrl={item.posterUrl}
          target={feedTarget(row, item)}
          testId={`discover-card-${row.id}-${item.kind}-${item.sourceId}`}
        />
      ))}
      <RowNotes notes={notes} />
      {items.length === 0 && notes.length === 0 && (
        <div className="flex w-full items-center">
          <EmptyState
            testId={`discover-row-empty-${row.id}`}
            title="Nothing here right now."
            message="Items from the feeds appear here as they become available."
          />
        </div>
      )}
    </DiscoverRow>
  )
}

// RecentlyAddedRow: the top ~12 library items across all four kinds.
function RecentlyAddedRow({
  row,
  recent,
}: {
  row: (typeof DISCOVER_ROWS)[number]
  recent: Loaded['recent'] | undefined
}) {
  if (!recent) {
    return (
      <DiscoverRow id={row.id} label={row.label}>
        <RowNotes notes={[LIST_UNAVAILABLE_NOTE]} />
      </DiscoverRow>
    )
  }
  const cards = buildRecentlyAdded(recent.movies, recent.series, recent.artists, recent.books)
  return (
    <DiscoverRow id={row.id} label={row.label}>
      {cards.map((c) => (
        <PosterCell
          key={`${c.kind}-${c.id}`}
          kind={c.kind}
          title={c.title}
          meta={c.meta}
          posterUrl={c.posterUrl}
          target={c.target}
          testId={`discover-card-${row.id}-${c.kind}-${c.id}`}
        />
      ))}
      {recent.failed.length > 0 && <RowNotes notes={[LIST_UNAVAILABLE_NOTE]} />}
      {cards.length === 0 && (
        <div className="flex w-full items-center">
          <EmptyState
            testId="discover-row-empty-recentlyAdded"
            title="No library items yet."
            message="Add your first movie, series, artist, or book and it shows up here."
          />
        </div>
      )}
    </DiscoverRow>
  )
}

// ActivityRow: the absorbed Dashboard recent queue + recent history feeds
// (landscape cards, newest first, capped at 10 by the model).
function ActivityRow({ row, dash }: { row: (typeof DISCOVER_ROWS)[number]; dash: Dashboard | null }) {
  const cards = mergeRecentActivity(dash)
  return (
    <DiscoverRow id={row.id} label={row.label}>
      {cards.map((c) => (
        <ActivityCardView key={c.key} card={c} testId={`discover-card-${row.id}-${c.key}`} />
      ))}
      {dash === null && <RowNotes notes={['Recent activity could not be loaded.']} />}
      {cards.length === 0 && dash !== null && (
        <div className="flex w-full items-center">
          <EmptyState
            testId="discover-row-empty-recentActivity"
            title="No recent activity."
            message="Queue updates and history events appear here."
          />
        </div>
      )}
    </DiscoverRow>
  )
}

// WatchlistRow: the monitored library items (the reference watchlist,
// redefined as monitored items), grouped by kind in the deterministic
// kind order.
function WatchlistRow({
  row,
  watch,
}: {
  row: (typeof DISCOVER_ROWS)[number]
  watch: Loaded['watch'] | undefined
}) {
  if (!watch) {
    return (
      <DiscoverRow id={row.id} label={row.label}>
        <RowNotes notes={[LIST_UNAVAILABLE_NOTE]} />
      </DiscoverRow>
    )
  }
  const cards = buildWatchlist(watch.movies, watch.series, watch.artists, watch.books)
  return (
    <DiscoverRow id={row.id} label={row.label}>
      {cards.map((c) => (
        <PosterCell
          key={`${c.kind}-${c.id}`}
          kind={c.kind}
          title={c.title}
          meta={c.meta}
          posterUrl={c.posterUrl}
          target={c.target}
          testId={`discover-card-${row.id}-${c.kind}-${c.id}`}
        />
      ))}
      {watch.failed.length > 0 && <RowNotes notes={[LIST_UNAVAILABLE_NOTE]} />}
      {cards.length === 0 && (
        <div className="flex w-full items-center">
          <EmptyState
            testId="discover-row-empty-watchlist"
            title="Your watchlist is empty."
            message="Monitor library items and they collect here."
          />
        </div>
      )}
    </DiscoverRow>
  )
}

// FacetRow: one facet row (genres / studios). The tiles are inert (divs):
// #150's list filters accept only a multi-value genre list (a single
// studio's movies can't be queried server-side) and the list pages don't
// read URL params, so a link would not filter — the tiles document what
// the library holds.
function FacetRow({
  row,
  facets,
  facetFailed,
}: {
  row: (typeof DISCOVER_ROWS)[number]
  facets: { name: string; count: number }[]
  facetFailed: boolean
}) {
  return (
    <DiscoverRow id={row.id} label={row.label}>
      {facets.map((f) => (
        <FacetTile key={f.name} name={f.name} count={f.count} testId={`discover-card-${row.id}-${f.name.replace(/\s+/g, '-').toLowerCase()}`} />
      ))}
      {facetFailed && <RowNotes notes={[LIST_UNAVAILABLE_NOTE]} />}
      {facets.length === 0 && (
        <div className="flex w-full items-center">
          <EmptyState
            testId={`discover-row-empty-${row.id}`}
            title={`No ${row.label.toLowerCase()} yet.`}
            message="These appear here once the library has items with them."
          />
        </div>
      )}
    </DiscoverRow>
  )
}

// ---- loading placeholders (the reference's 10-placeholder loading rows) ----------

function SkeletonStrip() {
  return (
    <div className="flex flex-wrap gap-2" data-testid="discover-kpi-loading">
      {Array.from({ length: 4 }, (_, i) => (
        <div key={i} className="h-16 min-w-28 flex-1 animate-pulse rounded-lg border border-border bg-surface-raised/40" />
      ))}
    </div>
  )
}

function RowSkeleton() {
  return (
    <>
      {Array.from({ length: 8 }, (_, i) => (
        <div key={i} className="w-36 shrink-0 animate-pulse rounded-lg border border-border bg-surface-raised/40">
          <div className="aspect-[2/3] w-full rounded-t-lg" />
        </div>
      ))}
    </>
  )
}
