import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { Book, ListQuery, PageAuthor } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { PosterGrid, type PosterItem } from '../../components/poster'
import {
  EmptyState,
  Menu,
  Toolbar,
  TbButton,
  ViewToggle,
  type ViewMode,
} from '../../components/ui'

// BooksPage is the Readarr B1 books library: a dual-view library.
//
//  - The **Authors** tab (the home view) is the server-side filtered,
//    paginated author list, rendered as a poster grid (with the A–Z
//    quick-jump) or a table. An author card opens the author detail page.
//  - The **Books** tab is the flat Books index — every book across all
//    authors — rendered as a poster grid (card = title, footer = author)
//    or a table (title + author + monitor toggle).
//
// The P7 toolbar (Update All / RSS Sync / Select / Options + Filter menu)
// and the shared ViewToggle carry over; "Select" carries the Author/Book
// Editor mass-edit affordance (disabled until the mass-edit backend lands).
const FILTER_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
] as const

export function BooksPage() {
  const [tab, setTab] = useState<'authors' | 'books'>('authors')

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
        <BooksIndexView key="books" />
      )}
    </div>
  )
}

// AuthorsView is the home view: the server-paginated author library in
// poster-grid or table form.
function AuthorsView() {
  const navigate = useNavigate()
  const load = useCallback((q: ListQuery) => client.listAuthors(q), [])
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
  } = usePagedList<PageAuthor>(load)

  const [view, setView] = useState<ViewMode>('grid')
  const [status, setStatus] = useState<string | null>(null)

  const items = useMemo(() => (data ? data.items : []), [data])

  const gridItems: PosterItem[] = items.map((a) => ({
    id: a.ID,
    name: a.Name,
    monitored: a.Monitored,
  }))

  function updateAll() {
    setStatus(`Updated ${total} authors just now`)
    reload()
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
            <ViewToggle view={view} onChange={setView} testId="books-view" />
            <Menu
              label="Filter"
              icon="⚲"
              options={[...FILTER_OPTIONS]}
              selected={monitored}
              onSelect={(v) => setMonitored(v as 'all' | 'monitored' | 'unmonitored')}
              testId="books-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="books-status" role="status">
          {status}
        </p>
      )}

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
              ) : items.length === 0 ? (
                <EmptyState
                  title="No authors"
                  message="Add your first author to get started."
                >
                  <Link to="/books/add" data-testid="books-empty-add">
                    + Add book
                  </Link>
                </EmptyState>
              ) : (
                <PosterGrid
                  items={gridItems}
                  testIdPrefix="author"
                  gridTestId="books-authors-grid"
                  azTestId="books-authors-az"
                  onOpen={(i) => navigate(`/books/${i.id}`)}
                />
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
              ) : items.length === 0 ? (
                <p className="dim" data-testid="books-empty">
                  No authors match.
                </p>
              ) : (
                items.map((a) => (
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

          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </>
      )}
    </>
  )
}

// BooksIndexView is the flat Books index: every book across all authors,
// client-filtered (monitored) and paginated (the endpoint returns the whole
// index; the library is bounded by the authors' titles).
function BooksIndexView() {
  const navigate = useNavigate()
  const [books, setBooks] = useState<Book[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [monitored, setMonitored] = useState<'all' | 'monitored' | 'unmonitored'>('all')
  const [view, setView] = useState<ViewMode>('grid')
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

  const all = useMemo(() => books ?? [], [books])
  const filtered = useMemo(() => {
    if (monitored === 'monitored') return all.filter((b) => b.Monitored)
    if (monitored === 'unmonitored') return all.filter((b) => !b.Monitored)
    return all
  }, [all, monitored])

  const total = filtered.length
  const pageCount = Math.max(1, Math.ceil(total / DEFAULT_PAGE_SIZE))
  const clampedPage = Math.min(page, pageCount)
  const rows = useMemo(
    () => filtered.slice((clampedPage - 1) * DEFAULT_PAGE_SIZE, clampedPage * DEFAULT_PAGE_SIZE),
    [filtered, clampedPage],
  )

  const gridItems: PosterItem[] = rows.map((b) => ({
    id: b.ID,
    name: b.Name,
    monitored: b.Monitored,
    quality: b.AuthorName,
  }))

  function updateAll() {
    setStatus(`Updated ${total} books just now`)
    load()
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
            <ViewToggle view={view} onChange={setView} testId="books-index-view" />
            <Menu
              label="Filter"
              icon="⚲"
              options={[...FILTER_OPTIONS]}
              selected={monitored}
              onSelect={(v) => {
                setMonitored(v as 'all' | 'monitored' | 'unmonitored')
                setPage(1)
              }}
              testId="books-index-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="books-index-status" role="status">
          {status}
        </p>
      )}

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
              ) : rows.length === 0 ? (
                <EmptyState
                  title="No books"
                  message={monitored === 'all' ? 'Add a book to get started.' : 'No books with this filter.'}
                >
                  {monitored === 'all' && (
                    <Link to="/books/add" data-testid="books-index-empty-add">
                      + Add book
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <PosterGrid
                  items={gridItems}
                  testIdPrefix="book"
                  gridTestId="books-grid"
                  azTestId="books-az"
                  onOpen={(i) => {
                    // A flat book opens its author's detail (the book lives in
                    // that author's accordion).
                    const b = rows.find((x) => x.ID === i.id)
                    navigate(`/books/${b?.AuthorID ?? i.id}`)
                  }}
                />
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="books-index-table">
              <div className="lt-head">
                <span>Title</span>
                <span>Author</span>
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

          <Pagination
            page={clampedPage}
            pageSize={DEFAULT_PAGE_SIZE}
            total={total}
            onPage={setPage}
          />
        </>
      )}
    </>
  )
}
