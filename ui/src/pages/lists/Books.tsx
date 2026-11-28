import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { client } from '../../api/client'
import type { Book, ListQuery, PageAuthor } from '../../api/types'
import { isRecentlyAdded } from '../books/Bookshelves'
import { AddButton } from '../../components/addflow'
import {
  FacetCheckboxGroup,
  FacetSelect,
  FilterDrawer,
  InfiniteSentinel,
  SortCluster,
  usePersistentView,
} from '../../components/gridcontrols'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
  type MonitoredFilter,
} from '../../components/list'
import { PosterCard } from '../../components/primitives/poster-card'
import { firstLetter } from '../../components/poster'
import { EmptyState, Toolbar, TbButton, ViewToggle } from '../../components/ui'

// BooksPage is the Readarr-parity books library, re-cast for #150.
//
//  - The **Authors** tab (the home view) is the server-side filtered,
//    paginated author list, rendered as a dense poster grid (real PosterURL
//    art with an initials stand-in, the kind pill, hover edit action) or a
//    table. The grid sorts and filters server-side and scrolls without
//    pagination chrome; the default sort is added-descending so a just-added
//    author leads the grid. An author card opens the author detail page.
//  - The **Books** tab is the flat Books index — every book across all
//    authors. The index endpoint returns the whole set (bounded by the
//    authors' titles) sorted by author then title, so its shelf filters
//    (monitored / missing / recent) are client-side; they live in the same
//    slide-over drawer as the server-side facets on the other tabs. The
//    grid shows the whole filtered index (no pagination chrome); the table
//    keeps page slicing.
//
// The P7 toolbar (Update All / RSS Sync / Select / Options + view toggle)
// carries over per tab; buttons without a backend are disabled stubs with a
// "later phase" title. The Readarr B4 Bookshelves click-through (?tab=books
// &monitored=…&missing=1&recent=1) lands on the matching drawer facets.
const MONITORED_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

// The index tab's shelf facets (client-side — the flat index endpoint takes
// no query params). 'missing' / 'recent' mirror the Bookshelves shelves.
type ShelfFacet = 'missing' | 'recent'

type FilterValue = 'all' | 'monitored' | 'unmonitored' | 'missing' | 'recent'

// initialFilterFromParams maps the Bookshelves click-through query params to
// the index tab's facets (missing/recent win over monitored; absent = all).
function initialShelfFromParams(params: URLSearchParams): {
  monitored: MonitoredFilter
  shelves: ShelfFacet[]
} {
  const shelves: ShelfFacet[] = []
  if (params.get('missing') === '1') shelves.push('missing')
  if (params.get('recent') === '1') shelves.push('recent')
  if (shelves.length > 0) return { monitored: 'all', shelves }
  if (params.get('monitored') === 'monitored') return { monitored: 'monitored', shelves }
  if (params.get('monitored') === 'unmonitored') return { monitored: 'unmonitored', shelves }
  return { monitored: 'all', shelves }
}

// indexFilterFromShelf reduces the drawer state back to a single effective
// filter for the client-side book predicate (missing/recent win over
// monitored, matching the legacy menu's behaviour).
function indexFilterFromShelf(monitored: MonitoredFilter, shelves: ShelfFacet[]): FilterValue {
  if (shelves.length > 0) return shelves[0]
  return monitored
}

export function BooksPage() {
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<'authors' | 'books'>(
    searchParams.get('tab') === 'books' ? 'books' : 'authors',
  )
  const initialShelf = initialShelfFromParams(searchParams)

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="books-title">Books</h2>
        <div className="dash-head-right">
          <AddButton to="/books/add" label="Add book" testId="books-add" />
        </div>
      </div>

      {/* The dual view: the Authors grid (home) and the flat Books index. */}
      <div
        className="view-toggle"
        role="group"
        aria-label="Library view"
        data-testid="books-tabs"
      >
        <button
          type="button"
          className={tab === 'authors' ? 'active' : ''}
          aria-pressed={tab === 'authors'}
          onClick={() => setTab('authors')}
          data-testid="books-tab-authors"
        >
          Authors
        </button>
        <button
          type="button"
          className={tab === 'books' ? 'active' : ''}
          aria-pressed={tab === 'books'}
          onClick={() => setTab('books')}
          data-testid="books-tab-books"
        >
          Books
        </button>
      </div>

      {tab === 'authors' ? (
        <AuthorsView key="authors" />
      ) : (
        <BooksIndexView key="books" initialShelf={initialShelf} />
      )}
    </div>
  )
}

// ---- Authors tab (server-paginated author library) ----

type AuthorSortField = 'title' | 'added'
type AuthorSortDir = 'asc' | 'desc'

// The sort dropdown fields (the direction is the separate toggle). Default
// is added-descending: a just-added author leads the grid (the e2e probe
// relies on the freshest author being visible on the first page).
const AUTHOR_SORT_FIELDS = [
  { value: 'added', label: 'Added' },
  { value: 'title', label: 'Name' },
] as const

function AuthorsView() {
  const navigate = useNavigate()
  const [view, setView] = usePersistentView('books-authors')
  const [sortField, setSortField] = useState<AuthorSortField>('added')
  const [sortDir, setSortDir] = useState<AuthorSortDir>('desc')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [status, setStatus] = useState<string | null>(null)

  const sort = `${sortField}-${sortDir}`
  const load = useCallback(
    (q: ListQuery) => client.listAuthors({ ...q, sort, filters: {} }),
    [sort],
  )
  const {
    items,
    data,
    loading,
    error,
    monitored,
    setMonitored,
    page,
    setPage,
    hasMore,
    loadMore,
    reload,
    total,
  } = usePagedList<PageAuthor>(load, { mode: 'append' })

  const activeCount = monitored === 'all' ? 0 : 1

  const gridItems = useMemo(() => items, [items])
  const listItems = useMemo(() => (data ? data.items : []), [data])
  const filtered = activeCount > 0

  function updateAll() {
    setStatus(`Updated ${total} authors just now`)
    reload()
  }

  function clearFilters() {
    setMonitored('all')
  }

  return (
    <>
      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="books-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="books-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Authors"
              testId="books-select"
              divider
              disabled
              title="Author bulk editing lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="books-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <SortCluster
              field={sortField}
              fields={[...AUTHOR_SORT_FIELDS]}
              dir={sortDir}
              onDir={setSortDir}
              onField={(f) => setSortField(f as AuthorSortField)}
              testId="books-authors"
            />
            <button
              type="button"
              className="menu-btn"
              onClick={() => setDrawerOpen(true)}
              aria-haspopup="dialog"
              data-testid="books-filter-btn"
            >
              <span aria-hidden>⚲</span>
              {activeCount} Active Filter{activeCount === 1 ? '' : 's'}
            </button>
            <ViewToggle view={view} onChange={setView} testId="books-view" />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="books-status" role="status">
          {status}
        </p>
      )}

      <FilterDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onClear={clearFilters} activeCount={activeCount}>
        <FacetSelect
          label="Monitored"
          value={monitored}
          options={MONITORED_OPTIONS}
          onChange={(v) => setMonitored(v as MonitoredFilter)}
          testId="books-facet-monitored"
        />
      </FilterDrawer>

      {error && !data ? (
        <div className="card" data-testid="books-error">
          <h3>Books unavailable</h3>
          <p className="error" data-testid="books-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !data ? (
                <p className="dim">Loading authors…</p>
              ) : gridItems.length === 0 ? (
                <EmptyState
                  title="No results"
                  message={
                    filtered
                      ? 'Try adjusting or clearing your filters.'
                      : 'Add your first author to get started.'
                  }
                >
                  {!filtered && (
                    <Link to="/books/add" data-testid="books-empty-add">
                      + Add book
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <>
                  <div className="poster-grid" data-testid="books-authors-grid">
                    {gridItems.map((a) => (
                      <div key={a.ID} data-poster-letter={firstLetter(a.Name)}>
                        <PosterCard
                          testId={`author-card-${a.ID}`}
                          title={a.Name}
                          image={a.PosterURL || undefined}
                          kind={{ label: 'Author' }}
                          onOpen={() => navigate(`/books/${a.ID}`)}
                          hoverActions={
                            <button
                              type="button"
                              className="icon-btn"
                              title="Edit author"
                              aria-label={`Edit ${a.Name}`}
                              data-testid={`card-edit-${a.ID}`}
                              onClick={() => navigate(`/books/${a.ID}`)}
                            >
                              ✎
                            </button>
                          }
                        />
                      </div>
                    ))}
                  </div>
                  {/* #150: no pagination chrome — the sentinel loads the
                      next server page as the grid's end scrolls in. */}
                  <InfiniteSentinel onLoadMore={loadMore} disabled={!hasMore} testId="books-authors-sentinel" />
                  {hasMore && loading && <p className="dim small">Loading more…</p>}
                </>
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="books-authors-table">
              <div className="lt-head">
                <span>Author</span>
                <span>Monitor</span>
              </div>
              {loading && !data ? (
                <p className="dim">Loading authors…</p>
              ) : listItems.length === 0 ? (
                <p className="dim" data-testid="books-empty">
                  No authors match.
                </p>
              ) : (
                listItems.map((a) => (
                  <div className="lt-row" key={a.ID} data-testid={`author-${a.ID}`}>
                    <span className="lt-name" title={a.Name}>
                      <Link to={`/books/${a.ID}`} data-testid={`author-link-${a.ID}`}>
                        {a.Name}
                      </Link>
                    </span>
                    <MonitoredToggle
                      monitored={a.Monitored}
                      onToggle={async (next) => {
                        await client.setAuthorMonitored(a.ID, next)
                        reload()
                      }}
                      label={`${a.Name} monitoring`}
                      testId={`author-mon-${a.ID}`}
                    />
                  </div>
                ))
              )}
            </div>
          )}

          {view === 'list' && (
            <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
          )}
        </>
      )}
    </>
  )
}

// ---- Books index tab (flat, client-filtered) ----

function BooksIndexView({
  initialShelf,
}: {
  initialShelf: { monitored: MonitoredFilter; shelves: ShelfFacet[] }
}) {
  const navigate = useNavigate()
  const [books, setBooks] = useState<Book[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [view, setView] = usePersistentView('books-index')
  const [monitored, setMonitored] = useState<MonitoredFilter>(initialShelf.monitored)
  const [shelves, setShelves] = useState<ShelfFacet[]>(initialShelf.shelves)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    client
      .listBooks()
      .then((b) => setBooks(b))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'failed to load books'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  // The drawer's active count: every set facet.
  const activeCount = (monitored === 'all' ? 0 : 1) + shelves.length
  const filtered = activeCount > 0

  // Client-side shelf predicate (the flat index endpoint takes no query
  // params). missing/recent win over monitored, matching the legacy menu.
  const all = useMemo(() => books ?? [], [books])
  const filteredBooks = useMemo(() => {
    switch (indexFilterFromShelf(monitored, shelves)) {
      case 'monitored':
        return all.filter((b) => b.Monitored)
      case 'unmonitored':
        return all.filter((b) => !b.Monitored)
      case 'missing':
        // Readarr B4 shelf: books with no imported file.
        return all.filter((b) => !b.HasFile)
      case 'recent':
        // Readarr B4 shelf: books added within the last 30 days.
        return all.filter((b) => isRecentlyAdded(b.AddedAt))
      default:
        return all
    }
  }, [all, monitored, shelves])

  const total = filteredBooks.length
  const pageCount = Math.max(1, Math.ceil(total / DEFAULT_PAGE_SIZE))
  const clampedPage = Math.min(page, pageCount)
  // The table slices; the grid renders the whole filtered index (the set is
  // bounded by the authors' titles, and #150 drops pagination chrome from
  // the grid).
  const rows = useMemo(
    () => filteredBooks.slice((clampedPage - 1) * DEFAULT_PAGE_SIZE, clampedPage * DEFAULT_PAGE_SIZE),
    [filteredBooks, clampedPage],
  )

  function updateAll() {
    setStatus(`Updated ${total} books just now`)
    load()
  }

  function clearFilters() {
    setMonitored('all')
    setShelves([])
    setPage(1)
  }

  function selectShelves(next: ShelfFacet[]) {
    setShelves(next)
    setPage(1)
  }

  return (
    <>
      <Toolbar
        left={
          <>
            <TbButton
              icon="⟳"
              label="Update All"
              testId="books-index-update-all"
              onClick={updateAll}
            />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="books-index-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Books"
              testId="books-index-select"
              divider
              disabled
              title="Book bulk editing lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="books-index-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <button
              type="button"
              className="menu-btn"
              onClick={() => setDrawerOpen(true)}
              aria-haspopup="dialog"
              data-testid="books-index-filter-btn"
            >
              <span aria-hidden>⚲</span>
              {activeCount} Active Filter{activeCount === 1 ? '' : 's'}
            </button>
            <ViewToggle view={view} onChange={setView} testId="books-index-view" />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="books-index-status" role="status">
          {status}
        </p>
      )}

      <FilterDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onClear={clearFilters} activeCount={activeCount}>
        <FacetSelect
          label="Monitored"
          value={monitored}
          options={MONITORED_OPTIONS}
          onChange={(v) => {
            setMonitored(v as MonitoredFilter)
            setPage(1)
          }}
          testId="books-index-facet-monitored"
        />
        <FacetCheckboxGroup
          label="Bookshelves"
          options={[
            { value: 'missing', label: 'Missing' },
            { value: 'recent', label: 'Recent (30 days)' },
          ]}
          values={shelves}
          onToggle={(values) => selectShelves(values as ShelfFacet[])}
          testId="books-index-facet-shelves"
        />
      </FilterDrawer>

      {error && !books ? (
        <div className="card" data-testid="books-index-error">
          <h3>Books unavailable</h3>
          <p className="error" data-testid="books-index-error-message">
            {error}
          </p>
          <button onClick={load}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !books ? (
                <p className="dim">Loading books…</p>
              ) : filteredBooks.length === 0 ? (
                <EmptyState
                  title="No results"
                  message={
                    filtered
                      ? 'Try adjusting or clearing your filters.'
                      : 'Add a book to get started.'
                  }
                >
                  {!filtered && (
                    <Link to="/books/add" data-testid="books-index-empty-add">
                      + Add book
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <>
                  <div className="poster-grid" data-testid="books-grid">
                    {filteredBooks.map((b) => (
                      <div key={b.ID} data-poster-letter={firstLetter(b.Name)}>
                        <PosterCard
                          testId={`book-card-${b.ID}`}
                          title={b.Name}
                          image={b.PosterURL || undefined}
                          meta={b.AuthorName}
                          status={b.HasFile ? undefined : { label: 'Missing', tone: 'error' }}
                          kind={{ label: 'Book' }}
                          onOpen={() => navigate(`/books/${b.AuthorID}`)}
                          hoverActions={
                            <button
                              type="button"
                              className="icon-btn"
                              title="Edit book"
                              aria-label={`Edit ${b.Name}`}
                              data-testid={`card-edit-${b.ID}`}
                              onClick={() => navigate(`/books/${b.AuthorID}`)}
                            >
                              ✎
                            </button>
                          }
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="books-index-table">
              <div className="lt-head">
                <span>Title</span>
                <span>Author</span>
                {/* Readarr B4: the per-book rollup (quality = resolvable
                    edition/file format, status = has file / missing). */}
                <span>Quality</span>
                <span>Status</span>
                <span>Monitor</span>
              </div>
              {loading && !books ? (
                <p className="dim">Loading books…</p>
              ) : rows.length === 0 ? (
                <p className="dim" data-testid="books-index-empty">
                  No books match.
                </p>
              ) : (
                rows.map((b) => (
                  <div className="lt-row" key={b.ID} data-testid={`book-${b.ID}`}>
                    <span className="lt-name" title={b.Name}>
                      <Link to={`/books/${b.AuthorID}`} data-testid={`book-link-${b.ID}`}>
                        {b.Name}
                      </Link>
                    </span>
                    <span className="lt-dim">{b.AuthorName}</span>
                    <span className="lt-dim" data-testid={`book-quality-${b.ID}`}>
                      {b.Format || '—'}
                    </span>
                    <span data-testid={`book-status-${b.ID}`}>
                      {b.HasFile ? (
                        <span className="badge badge-ok">Has file</span>
                      ) : (
                        <span className="badge badge-err">Missing</span>
                      )}
                    </span>
                    <MonitoredToggle
                      monitored={b.Monitored}
                      onToggle={async (next) => {
                        await client.setTitleMonitored(b.ID, next)
                        load()
                      }}
                      label={`${b.Name} monitoring`}
                      testId={`book-mon-${b.ID}`}
                    />
                  </div>
                ))
              )}
            </div>
          )}

          {view === 'list' && (
            <Pagination
              page={clampedPage}
              pageSize={DEFAULT_PAGE_SIZE}
              total={total}
              onPage={setPage}
            />
          )}
        </>
      )}
    </>
  )
}
