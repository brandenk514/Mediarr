import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { client } from '../../api/client'
import type {
  BookAuthorSeries,
  BookFile,
  BookHistoryEntry,
  BookSeries,
  Edition,
  Author,
  Title,
} from '../../api/types'
import { posterText } from '../../components/poster'
import { relTime } from '../../components/music'
import { MonitoredToggle } from '../../components/list'
import {
  Badge,
  BadgeRow,
  Modal,
  Toolbar,
  TbButton,
} from '../../components/ui'

const DASH = '—'

function humanBytes(n: number): string {
  if (n <= 0) return DASH
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// AuthorDetailPage is the Readarr B1 author detail: a header card (poster
// placeholder, name, monitor, series-membership badges) above a books
// accordion. Each book expands to its editions — an edition is the books
// analogue of Radarr's edition model, the (format, language, monitored)
// triple — and below the editions the imported book files with their media
// info (pages, size, format, language, added date). A toolbar carries
// Refresh & Scan, Search (the author pipeline), History (modal), and the
// not-yet-built Edit/Delete.
export function AuthorDetailPage() {
  const { id } = useParams<{ id: string }>()
  const authorId = Number(id)

  const [author, setAuthor] = useState<Author | null>(null)
  const [titles, setTitles] = useState<Title[] | null>(null)
  const [series, setSeries] = useState<BookAuthorSeries[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [searching, setSearching] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [modal, setModal] = useState<null | 'history'>(null)

  const reload = useCallback(() => {
    setError(null)
    Promise.all([
      client.getAuthor(authorId),
      client.listTitles(authorId),
      client.listAuthorSeries(authorId),
    ])
      .then(([a, t, s]) => {
        setAuthor(a)
        setTitles(t)
        setSeries(s)
      })
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : 'failed to load author'),
      )
  }, [authorId])

  useEffect(() => {
    reload()
  }, [reload])

  // Search runs the author's indexer pipeline (the books analogue of Radarr's
  // "Search" command) and reports the import result.
  async function search() {
    setSearching(true)
    setStatus('Searching releases…')
    try {
      const res = await client.runBooksPipeline(authorId)
      setStatus(res.imported || 'Search complete — no new releases imported.')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'release search failed')
    } finally {
      setSearching(false)
    }
  }

  if (error && !author) {
    return (
      <div className="card" data-testid="author-detail-error">
        <h2>Author unavailable</h2>
        <p className="error" data-testid="author-detail-error-message">
          {error}
        </p>
        <button onClick={() => void reload()}>Retry</button>
      </div>
    )
  }

  if (!author) {
    return (
      <p className="dim" data-testid="author-detail-loading">
        Loading author…
      </p>
    )
  }

  return (
    <div className="list-page" data-testid="author-detail">
      <div className="page-head">
        <Link to="/books" className="ghost" data-testid="author-detail-back">
          ← Books
        </Link>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh & Scan" testId="author-detail-refresh" onClick={() => void reload()} />
            <TbButton
              icon="🔍"
              label="Search"
              testId="author-detail-search"
              disabled={searching}
              onClick={() => void search()}
            />
            <TbButton
              icon="🕘"
              label="History"
              testId="author-detail-history"
              divider
              onClick={() => setModal('history')}
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="✏"
              label="Edit"
              testId="author-detail-edit"
              disabled
              title="Author editing lands in a later phase"
            />
            <TbButton
              icon="🗑"
              label="Delete"
              testId="author-detail-delete"
              divider
              disabled
              title="Author deletion lands in a later phase"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="author-detail-status" role="status">
          {status}
        </p>
      )}

      {/* Header card: poster placeholder + name + monitor + badges. */}
      <div className="card" data-testid="author-detail-header">
        <div className="detail-header">
          <div className="detail-poster" aria-hidden data-testid="author-detail-poster">
            {posterText(author.Name)}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div className="detail-title">
              <h2 data-testid="author-detail-name">{author.Name}</h2>
              <MonitoredToggle
                monitored={author.Monitored}
                onToggle={async (next) => {
                  await client.setAuthorMonitored(authorId, next)
                  setAuthor({ ...author, Monitored: next })
                }}
                label={`${author.Name} monitoring`}
                testId="author-detail-monitored"
              />
            </div>

            <BadgeRow>
              <Badge tone={author.Monitored ? 'ok' : 'default'} testId="badge-monitored">
                {author.Monitored ? 'Monitored' : 'Unmonitored'}
              </Badge>
              <Badge testId="badge-book-count">
                {titles ? `${titles.length} book${titles.length === 1 ? '' : 's'}` : ''}
              </Badge>
              {series && series.length > 0
                ? series.map((s) => (
                    <Badge key={s.ID} tone="teal" testId={`badge-series-${s.ID}`}>
                      {s.Name}
                    </Badge>
                  ))
                : null}
            </BadgeRow>

            <div className="field-grid" data-testid="author-detail-fields">
              <div>
                <span className="fg-label">Books</span>
                <span className="fg-value" data-testid="field-books">
                  {titles ? titles.length : DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Added</span>
                <span className="fg-value" data-testid="field-added">
                  {author.AddedAt}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <BooksAccordion titles={titles ?? []} onToggle={reload} />

      {/* History modal (the per-author history feed; books don't flow into
          the global feed yet). */}
      {modal === 'history' && <HistoryModal authorId={authorId} onClose={() => setModal(null)} />}
    </div>
  )
}

// BooksAccordion is the expandable books list. Each book row expands to the
// title monitor toggle + series-membership badges, the edition list (the
// (format, language, monitored) triples), and the imported book files with
// their media info.
function BooksAccordion({ titles, onToggle }: { titles: Title[]; onToggle: () => void }) {
  const [open, setOpen] = useState<ReadonlySet<number>>(new Set())

  function toggle(id: number) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (titles.length === 0)
    return (
      <div className="card" data-testid="author-detail-books-empty">
        <h3>Books</h3>
        <p className="dim">No books yet. Add one from the library page.</p>
      </div>
    )

  return (
    <div data-testid="author-detail-books">
      {titles.map((t) => {
        const expanded = open.has(t.ID)
        return (
          <div className="type-group" key={t.ID} data-testid={`book-accordion-${t.ID}`}>
            <div className="type-group-head">
              <button
                type="button"
                aria-expanded={expanded}
                onClick={() => toggle(t.ID)}
                data-testid={`book-expand-${t.ID}`}
                style={{
                  border: 'none',
                  background: 'none',
                  cursor: 'pointer',
                  color: 'var(--dim)',
                  fontSize: '0.9rem',
                  padding: 0,
                }}
              >
                {expanded ? '▾' : '▸'}
              </button>
              <span>{t.Name}</span>
              <span className="tg-size" data-testid={`book-added-${t.ID}`}>
                {t.AddedAt}
              </span>
              <MonitoredToggle
                monitored={t.Monitored}
                onToggle={async (next) => {
                  await client.setTitleMonitored(t.ID, next)
                  onToggle()
                }}
                label={`${t.Name} monitoring`}
                testId={`book-mon-${t.ID}`}
              />
            </div>

            {expanded && (
              <div className="album-sub" data-testid={`book-detail-${t.ID}`}>
                <TitleSeries titleId={t.ID} />
                <TitleEditions titleId={t.ID} onToggle={onToggle} />
                <TitleFiles titleId={t.ID} />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// TitleSeries shows the series-membership badges for a book (the many-to-many
// book↔series link surface).
function TitleSeries({ titleId }: { titleId: number }) {
  const [list, setList] = useState<BookSeries[] | null>(null)

  useEffect(() => {
    client
      .listTitleSeries(titleId)
      .then(setList)
      .catch(() => setList([]))
  }, [titleId])

  if (!list || list.length === 0) return null
  return (
    <span data-testid={`book-series-${titleId}`}>
      {list.map((s) => (
        <Badge key={s.ID} tone="teal" testId={`book-series-badge-${s.ID}`}>
          {s.Name}
        </Badge>
      ))}
    </span>
  )
}

// TitleEditions lists a book's editions. Each edition carries the B1 triple —
// format (EPUB/MOBI/AZW3), language, and a per-edition monitored flag.
function TitleEditions({ titleId, onToggle }: { titleId: number; onToggle: () => void }) {
  const [editions, setEditions] = useState<Edition[] | null>(null)

  useEffect(() => {
    client
      .listEditions(titleId)
      .then(setEditions)
      .catch(() => setEditions([]))
  }, [titleId])

  if (!editions) return null

  return (
    <div data-testid={`book-editions-${titleId}`}>
      <h4>Editions</h4>
      {editions.length === 0 ? (
        <p className="dim" data-testid={`book-editions-empty-${titleId}`}>
          No editions yet. Editions are added manually (Open Library resolution
          lands in a later phase).
        </p>
      ) : (
        editions.map((e) => (
          <div className="edition-row" key={e.ID} data-testid={`edition-${e.ID}`}>
            <span className="ed-label">
              <Badge tone="teal">{e.Format.toUpperCase()}</Badge>
            </span>
            <span className="ed-meta">
              {e.Language || DASH}
              {e.Year > 0 ? ` · ${e.Year}` : ''}
              {e.Pages > 0 ? ` · ${e.Pages} pp` : ''}
              {e.ISBN ? ` · ISBN ${e.ISBN}` : ''}
            </span>
            <MonitoredToggle
              monitored={e.Monitored}
              onToggle={async (next) => {
                await client.setEditionMonitored(e.ID, next)
                onToggle()
              }}
              label={`${e.Format} edition monitoring`}
              testId={`edition-mon-${e.ID}`}
            />
          </div>
        ))
      )}
    </div>
  )
}

// TitleFiles is the book-files table: the imported e-book files with their
// media info (pages, size, format, language, added date).
function TitleFiles({ titleId }: { titleId: number }) {
  const [files, setFiles] = useState<BookFile[] | null>(null)

  useEffect(() => {
    client
      .listBookFiles(titleId)
      .then(setFiles)
      .catch(() => setFiles([]))
  }, [titleId])

  if (!files) return null

  return (
    <div data-testid={`book-files-${titleId}`}>
      <h4>Files</h4>
      {files.length === 0 ? (
        <p className="dim" data-testid={`book-files-empty-${titleId}`}>
          No book files yet.
        </p>
      ) : (
        <table className="import-table" data-testid={`book-files-table-${titleId}`}>
          <thead>
            <tr>
              <th>Path</th>
              <th>Format</th>
              <th>Language</th>
              <th>Pages</th>
              <th>Size</th>
              <th>Added</th>
            </tr>
          </thead>
          <tbody>
            {files.map((f, i) => (
              <tr key={f.ID} data-testid={`book-file-row-${i}`}>
                <td>{f.Path}</td>
                <td>{f.Format || DASH}</td>
                <td>{f.Language || DASH}</td>
                <td>{f.Pages > 0 ? f.Pages : DASH}</td>
                <td>{humanBytes(f.Size)}</td>
                <td>{f.AddedAt}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// HistoryModal is the per-author history feed (the books analogue of the
// shared P3 history surface; books don't flow into the global feed yet).
function HistoryModal({ authorId, onClose }: { authorId: number; onClose: () => void }) {
  const [entries, setEntries] = useState<BookHistoryEntry[] | null>(null)

  useEffect(() => {
    client
      .listAuthorHistory(authorId)
      .then(setEntries)
      .catch(() => setEntries([]))
  }, [authorId])

  return (
    <Modal title="History" onClose={onClose} testId="history-modal">
      {entries === null ? (
        <p className="dim">Loading history…</p>
      ) : entries.length === 0 ? (
        <p className="dim" data-testid="author-history-empty">
          No history yet.
        </p>
      ) : (
        entries.map((h, i) => (
          <div className="history-row" key={h.ID} data-testid={`history-${i}`}>
            <span className="hr-event">{h.Event}</span>
            <span className="hr-detail">{h.Detail}</span>
            <span className="hr-at">{relTime(h.At)}</span>
          </div>
        ))
      )}
    </Modal>
  )
}
