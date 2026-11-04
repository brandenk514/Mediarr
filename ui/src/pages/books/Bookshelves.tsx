import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { client } from '../../api/client'
import type { Book, BookSeries, BookSeriesBook } from '../../api/types'
import { EmptyState } from '../../components/ui'

// BookshelvesPage is the Readarr B4 bookshelves page — the books analogue of
// Radarr's bookshelves: a client-side aggregation over the flat Books index
// (GET /books) + the series surface (GET /books/series + per-series member
// lists), grouped into named "shelves" each carrying the plan's rollup
// (book count, missing = no files, monitored, and quality — the resolvable
// edition/file format, 'Mixed' when members disagree, '—' when unknown).
//
// A shelf is a filtered/grouped view with counts; clicking through lands on
// the existing pages: the Books list (with the matching shelf filter) for the
// standard shelves, and the series detail for the per-series shelves. No
// backend shelves persistence — series ARE the custom-group mechanism.

// RECENT_WINDOW_MS is the "Recent" shelf window: books added in the last 30
// days (by the index row's AddedAt).
const RECENT_WINDOW_MS = 30 * 24 * 60 * 60 * 1000

// Shelf is one bookshelf: a named view with its rollup + click-through target.
export interface Shelf {
  key: string
  name: string
  /** The existing page the shelf clicks through to. */
  to: string
  books: number
  missing: number
  monitored: number
  quality: string
}

// isRecentlyAdded reports whether a book was added within the Recent window.
export function isRecentlyAdded(addedAt: string, now: number = Date.now()): boolean {
  const t = new Date(addedAt).getTime()
  return Number.isFinite(t) && now - t <= RECENT_WINDOW_MS
}

// qualityOf folds a shelf's member formats into the rollup label: a single
// format when all resolvable members agree, 'Mixed' when they disagree, '—'
// when none is resolvable (the Collections page's aggregate pattern).
export function qualityOf(books: Pick<Book, 'Format'>[]): string {
  const uniq = [...new Set(books.map((b) => b.Format).filter(Boolean))]
  if (uniq.length === 0) return '—'
  return uniq.length === 1 ? uniq[0]! : 'Mixed'
}

// rollup folds a member set into the shelf's counts (missing = no files).
function rollup(books: Array<Pick<Book, 'HasFile' | 'Monitored' | 'Format'>>): Pick<
  Shelf,
  'books' | 'missing' | 'monitored' | 'quality'
> {
  return {
    books: books.length,
    missing: books.filter((b) => !b.HasFile).length,
    monitored: books.filter((b) => b.Monitored).length,
    quality: qualityOf(books),
  }
}

export function BookshelvesPage() {
  const [books, setBooks] = useState<Book[] | null>(null)
  const [series, setSeries] = useState<BookSeries[] | null>(null)
  const [members, setMembers] = useState<Record<number, BookSeriesBook[]> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    client
      .listBooks()
      .then(async (b) => {
        const s = await client.listBookSeries()
        // Client-side aggregation (the plan's "acceptable first" bookshelf
        // grouping): one member-list request per series, in parallel.
        const perSeries = await Promise.all(
          s.map(async (ser) => {
            try {
              return [ser.ID, await client.listSeriesBooks(ser.ID)] as const
            } catch {
              return [ser.ID, [] as BookSeriesBook[]] as const
            }
          }),
        )
        setBooks(b)
        setSeries(s)
        setMembers(Object.fromEntries(perSeries))
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'failed to load bookshelves'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const shelves = useMemo<Shelf[]>(() => {
    if (!books || !series || !members) return []
    const all = books
    const recent = all.filter((b) => isRecentlyAdded(b.AddedAt))
    const standard: Shelf[] = [
      { key: 'all', name: 'All Books', to: '/books?tab=books', ...rollup(all) },
      {
        key: 'monitored',
        name: 'Monitored',
        to: '/books?tab=books&monitored=monitored',
        ...rollup(all.filter((b) => b.Monitored)),
      },
      {
        key: 'unmonitored',
        name: 'Unmonitored',
        to: '/books?tab=books&monitored=unmonitored',
        ...rollup(all.filter((b) => !b.Monitored)),
      },
      {
        key: 'missing',
        name: 'Missing',
        to: '/books?tab=books&missing=1',
        ...rollup(all.filter((b) => !b.HasFile)),
      },
      { key: 'recent', name: 'Recent (30 days)', to: '/books?tab=books&recent=1', ...rollup(recent) },
    ]
    // By Series: each series is a shelf of its member books (members come
    // from the per-series member lists, which carry the rollup fields).
    const bySeries: Shelf[] = series.map((s) => ({
      key: `series-${s.ID}`,
      name: s.Name,
      to: `/books/series/${s.ID}`,
      ...rollup(members[s.ID] ?? []),
    }))
    return [...standard, ...bySeries]
  }, [books, series, members])

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="bookshelves-title">Bookshelves</h2>
      </div>

      {error && !books ? (
        <div className="card" data-testid="bookshelves-error">
          <h3>Bookshelves unavailable</h3>
          <p className="error" data-testid="bookshelves-error-message">
            {error}
          </p>
          <button onClick={load}>Retry</button>
        </div>
      ) : loading && !books ? (
        <p className="dim" data-testid="bookshelves-loading">
          Loading bookshelves…
        </p>
      ) : books && books.length === 0 && (series?.length ?? 0) === 0 ? (
        <EmptyState
          testId="bookshelves-empty"
          title="No bookshelves"
          message="Books and series you track show up here as shelves."
        >
          <Link to="/books/add" data-testid="bookshelves-empty-add">
            + Add book
          </Link>
        </EmptyState>
      ) : (
        <div className="collections-grid" data-testid="bookshelves-grid">
          {shelves.map((s) => (
            <ShelfCard key={s.key} shelf={s} />
          ))}
        </div>
      )}
    </div>
  )
}

// ShelfCard is one bookshelf: the name, the rollup line (N books · M missing
// · K monitored), the quality label, and the click-through link.
function ShelfCard({ shelf }: { shelf: Shelf }) {
  return (
    <section className="card collection-card" data-testid={`bookshelf-${shelf.key}`}>
      <h3>
        <Link to={shelf.to} data-testid={`bookshelf-link-${shelf.key}`}>
          {shelf.name}
        </Link>
      </h3>
      <p className="dim small" data-testid={`bookshelf-count-${shelf.key}`}>
        {shelf.books} book{shelf.books === 1 ? '' : 's'} · {shelf.missing} missing ·{' '}
        {shelf.monitored} monitored
      </p>
      <p className="dim small" data-testid={`bookshelf-quality-${shelf.key}`}>
        Quality: {shelf.quality}
      </p>
    </section>
  )
}
