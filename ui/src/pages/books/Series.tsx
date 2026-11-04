import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { client } from '../../api/client'
import type { Book, BookSeries, BookSeriesBook } from '../../api/types'
import { EmptyState, Modal } from '../../components/ui'
import { qualityOf } from './Bookshelves'

// Series pages (Readarr B4) — the books analogue of Radarr's Collections:
//
//  - SeriesIndexPage (/books/series): every series as a card with its member
//    rollup (N members · M missing · K monitored + quality), a click-through
//    to the detail page, and the create-series flow (POST /books/series,
//    idempotent by name).
//  - SeriesDetailPage (/books/series/:id): the series' member books ACROSS
//    AUTHORS (the author name per row is the point of this page — an author's
//    own series list only shows that author's titles), each with its
//    missing/quality/monitored rollup, plus the link-a-book flow (POST
//    /books/titles/{id}/series) so any library book can be added.
//
// Both pages are client-side aggregations over the B1 series surface:
// listBookSeries + one listSeriesBooks per series (no backend endpoint).

interface SeriesRollup {
  members: number
  missing: number
  monitored: number
  quality: string
}

// seriesRollup folds a member list into the card/detail header rollup.
function seriesRollup(members: BookSeriesBook[]): SeriesRollup {
  return {
    members: members.length,
    missing: members.filter((m) => !m.HasFile).length,
    monitored: members.filter((m) => m.Monitored).length,
    quality: qualityOf(members),
  }
}

// loadSeriesWithMembers fetches every series and its members in one shot
// (the client-side aggregation the bookshelves phase budgets for).
async function loadSeriesWithMembers(): Promise<{ series: BookSeries[]; members: Record<number, BookSeriesBook[]> }> {
  const series = await client.listBookSeries()
  const perSeries = await Promise.all(
    series.map(async (s) => {
      try {
        return [s.ID, await client.listSeriesBooks(s.ID)] as const
      } catch {
        return [s.ID, [] as BookSeriesBook[]] as const
      }
    }),
  )
  return { series, members: Object.fromEntries(perSeries) }
}

// SeriesIndexPage lists every series with its member rollup.
export function SeriesIndexPage() {
  const [series, setSeries] = useState<BookSeries[] | null>(null)
  const [members, setMembers] = useState<Record<number, BookSeriesBook[]> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    loadSeriesWithMembers()
      .then(({ series, members }) => {
        setSeries(series)
        setMembers(members)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'failed to load series'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="book-series-title">Series</h2>
        <div className="dash-head-right">
          <button
            type="button"
            className="ghost"
            onClick={() => setCreating(true)}
            data-testid="book-series-new"
          >
            + New series
          </button>
        </div>
      </div>

      {creating && <CreateSeriesModal onClose={() => setCreating(false)} onCreated={load} />}

      {error && !series ? (
        <div className="card" data-testid="book-series-error">
          <h3>Series unavailable</h3>
          <p className="error" data-testid="book-series-error-message">
            {error}
          </p>
          <button onClick={load}>Retry</button>
        </div>
      ) : loading && !series ? (
        <p className="dim" data-testid="book-series-loading">
          Loading series…
        </p>
      ) : series && series.length === 0 ? (
        <EmptyState
          testId="book-series-empty"
          title="No series"
          message="Group books into series (e.g. a book cycle) to track them as a collection."
        >
          <button type="button" className="ghost" onClick={() => setCreating(true)} data-testid="book-series-empty-new">
            + New series
          </button>
        </EmptyState>
      ) : (
        <div className="collections-grid" data-testid="book-series-grid">
          {series!.map((s) => {
            const r = seriesRollup(members?.[s.ID] ?? [])
            return (
              <section className="card collection-card" key={s.ID} data-testid={`book-series-card-${s.ID}`}>
                <h3>
                  <Link to={`/books/series/${s.ID}`} data-testid={`book-series-link-${s.ID}`}>
                    {s.Name}
                  </Link>
                </h3>
                <p className="dim small" data-testid={`book-series-members-${s.ID}`}>
                  {r.members} member{r.members === 1 ? '' : 's'} · {r.missing} missing · {r.monitored} monitored
                </p>
                <p className="dim small" data-testid={`book-series-quality-${s.ID}`}>
                  Quality: {r.quality}
                </p>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}

// CreateSeriesModal is the create-series flow: a name, POST /books/series
// (idempotent by name — an existing series comes back as its id), and a
// reload so the new card appears.
function CreateSeriesModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function create() {
    const trimmed = name.trim()
    if (!trimmed || busy) return
    setBusy(true)
    setError(null)
    try {
      await client.addBookSeries({ name: trimmed })
      onCreated()
      onClose()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'failed to create series')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="New series"
      onClose={onClose}
      testId="book-series-modal"
      footer={
        <>
          <button type="button" className="ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="button" onClick={create} disabled={!name.trim() || busy} data-testid="book-series-create">
            Create
          </button>
        </>
      }
    >
      <label htmlFor="book-series-name">
        Name
        <input
          id="book-series-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. Earthsea"
          data-testid="book-series-name-input"
        />
      </label>
      {error && (
        <p className="error" data-testid="book-series-modal-error">
          {error}
        </p>
      )}
    </Modal>
  )
}

// SeriesDetailPage shows one series' member books across authors: title
// (linking to the author's detail page, where the book lives), the author
// name (the point of this page), quality (resolvable format), missing state,
// and monitor state — plus the link-a-book control.
export function BookSeriesDetailPage() {
  const { id } = useParams<{ id: string }>()
  const seriesId = Number(id)
  const [series, setSeries] = useState<BookSeries | null>(null)
  const [members, setMembers] = useState<BookSeriesBook[] | null>(null)
  const [allBooks, setAllBooks] = useState<Book[]>([])
  const [selected, setSelected] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [status, setStatus] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    Promise.all([
      client.getBookSeries(seriesId),
      client.listSeriesBooks(seriesId),
      client.listBooks().catch(() => [] as Book[]),
    ])
      .then(([s, m, all]) => {
        setSeries(s)
        setMembers(m)
        setAllBooks(all)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'failed to load series'))
      .finally(() => setLoading(false))
  }, [seriesId])

  useEffect(() => {
    load()
  }, [load])

  async function linkBook() {
    const book = allBooks.find((b) => b.ID === selected)
    if (!book || !series) return
    setStatus(null)
    try {
      await client.linkTitleToSeries(book.ID, series.ID)
      const m = await client.listSeriesBooks(series.ID)
      setMembers(m)
      setStatus(`Linked "${book.Name}" to ${series.Name}.`)
    } catch (e: unknown) {
      setStatus(e instanceof Error ? e.message : 'failed to link book')
    }
  }

  if (error) {
    return (
      <div className="list-page">
        <div className="card" data-testid="book-series-detail-error">
          <h3>Series unavailable</h3>
          <p className="error" data-testid="book-series-detail-error-message">
            {error}
          </p>
          <button onClick={load}>Retry</button>
        </div>
      </div>
    )
  }

  const rollup = seriesRollup(members ?? [])

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="book-series-detail-title">
          <Link to="/books/series" data-testid="book-series-detail-back">
            ← All series
          </Link>{' '}
          <span className="dim">/</span> {series?.Name ?? '…'}
        </h2>
      </div>

      {loading && !series ? (
        <p className="dim">Loading series…</p>
      ) : (
        <>
          <p className="dim" data-testid="book-series-detail-rollup">
            {rollup.members} member{rollup.members === 1 ? '' : 's'} · {rollup.missing} missing ·{' '}
            {rollup.monitored} monitored · Quality: {rollup.quality}
          </p>

          {/* Link-a-book: pick any library book, link it into this series
              (idempotent on the backend). */}
          {allBooks.length > 0 && (
            <div className="card" data-testid="book-series-link-card">
              <h3>Link a book</h3>
              <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                <select
                  value={selected}
                  onChange={(e) => setSelected(Number(e.target.value))}
                  data-testid="book-series-link-select"
                  aria-label="Book to link"
                >
                  {allBooks.map((b) => (
                    <option key={b.ID} value={b.ID}>
                      {b.Name} — {b.AuthorName}
                    </option>
                  ))}
                </select>
                <button type="button" onClick={linkBook} data-testid="book-series-link-btn">
                  Link
                </button>
              </div>
              {status && (
                <p className="dim small" data-testid="book-series-link-status" role="status">
                  {status}
                </p>
              )}
            </div>
          )}

          {members && members.length === 0 ? (
            <EmptyState
              testId="book-series-detail-empty"
              title="No member books"
              message="Link a book above, or from a book's detail page on the author."
            />
          ) : (
            <div className="list-table" data-testid="book-series-members">
              <div className="lt-head">
                <span>Title</span>
                <span>Author</span>
                <span>Quality</span>
                <span>Status</span>
                <span>Monitor</span>
              </div>
              {(members ?? []).map((m) => (
                <div className="lt-row" key={m.ID} data-testid={`book-series-member-${m.ID}`}>
                  <span className="lt-name" title={m.Name}>
                    <Link to={`/books/${m.AuthorID}`} data-testid={`book-series-member-link-${m.ID}`}>
                      {m.Name}
                    </Link>
                  </span>
                  <span className="lt-dim" data-testid={`book-series-member-author-${m.ID}`}>
                    {m.AuthorName}
                  </span>
                  <span className="lt-dim" data-testid={`book-series-member-quality-${m.ID}`}>
                    {m.Format || '—'}
                  </span>
                  <span data-testid={`book-series-member-status-${m.ID}`}>
                    {m.HasFile ? (
                      <span className="badge badge-ok">Has file</span>
                    ) : (
                      <span className="badge badge-err">Missing</span>
                    )}
                  </span>
                  <span data-testid={`book-series-member-monitored-${m.ID}`}>
                    {m.Monitored ? (
                      <span className="badge badge-teal">Monitored</span>
                    ) : (
                      <span className="badge">Unmonitored</span>
                    )}
                  </span>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
