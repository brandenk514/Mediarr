import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { Edition, ListQuery, PageAuthor, Title } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { Menu, Toolbar, TbButton } from '../../components/ui'

// BooksPage is the P7 books library — Readarr-shaped within the shared shell:
// a server-side filtered, paginated author list where each author expands to
// its titles and each title expands to its editions (both fetched lazily on
// expand). P7 brings the shared toolbar (Update All / RSS Sync / Select /
// Options + Filter menu) so all four kinds share the same shell pattern.
type MonitoredFilter = 'all' | 'monitored' | 'unmonitored'

const FILTER_OPTIONS: { value: MonitoredFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

export function BooksPage() {
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
    expanded,
    toggleExpanded,
  } = usePagedList<PageAuthor>(load)

  const [status, setStatus] = useState<string | null>(null)

  function updateAll() {
    setStatus(`Updated ${total} authors just now`)
    reload()
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="books-title">Books</h2>
        <div className="dash-head-right">
          <AddButton to="/books/add" label="Add book" testId="books-add" />
        </div>
      </div>

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
              title="Bulk selection lands in a later phase"
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
            <Menu
              label="Filter"
              icon="⚲"
              options={[...FILTER_OPTIONS]}
              selected={monitored}
              onSelect={(v) => setMonitored(v as MonitoredFilter)}
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
        <div className="list-list" data-testid="books-list">
          {loading && !data ? (
            <p className="dim">Loading authors…</p>
          ) : data && data.items.length === 0 ? (
            <p className="dim" data-testid="books-empty">
              No authors match.
            </p>
          ) : (
            data?.items.map((a) => (
              <div className="list-item" key={a.ID} data-testid={`author-${a.ID}`}>
                <div className="item-head">
                  <button
                    type="button"
                    className={`row-head ${expanded.has(a.ID) ? 'open' : ''}`}
                    onClick={() => toggleExpanded(a.ID)}
                    aria-expanded={expanded.has(a.ID)}
                    data-testid={`author-toggle-${a.ID}`}
                  >
                    <span className={`chevron ${expanded.has(a.ID) ? 'chevron-open' : ''}`}>
                      {expanded.has(a.ID) ? '▾' : '▸'}
                    </span>
                    <span className="lt-name" title={a.Name}>
                      {a.Name}
                    </span>
                  </button>
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
                {expanded.has(a.ID) && <TitleList authorId={a.ID} />}
              </div>
            ))
          )}
          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </div>
      )}
    </div>
  )
}

// TitleList fetches an author's titles on expand; each title expands to editions.
function TitleList({ authorId }: { authorId: number }) {
  const [titles, setTitles] = useState<Title[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [open, setOpen] = useState<Set<number>>(() => new Set())

  useEffect(() => {
    let cancelled = false
    client
      .listTitles(authorId)
      .then((t) => !cancelled && setTitles(t))
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load titles'))
    return () => {
      cancelled = true
    }
  }, [authorId])

  function toggleTitle(id: number) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (err) return <p className="error">Title error: {err}</p>
  if (!titles) return <p className="dim">Loading titles…</p>
  if (titles.length === 0) return <p className="dim">No titles yet.</p>
  return (
    <div className="child-list" data-testid={`titles-${authorId}`}>
      {titles.map((t) => (
        <div className="child-item" key={t.ID}>
          <div className="item-head">
            <button
              type="button"
              className={`row-head small ${open.has(t.ID) ? 'open' : ''}`}
              onClick={() => toggleTitle(t.ID)}
              aria-expanded={open.has(t.ID)}
              data-testid={`title-toggle-${t.ID}`}
            >
              <span className={`chevron ${open.has(t.ID) ? 'chevron-open' : ''}`}>
                {open.has(t.ID) ? '▾' : '▸'}
              </span>
              <span className="lt-name">{t.Name}</span>
            </button>
            <MonitoredToggle
              monitored={t.Monitored}
              onToggle={async (next) => {
                await client.setTitleMonitored(t.ID, next)
              }}
              label={`${t.Name} monitoring`}
              testId={`title-mon-${t.ID}`}
            />
          </div>
          {open.has(t.ID) && <EditionList titleId={t.ID} />}
        </div>
      ))}
    </div>
  )
}

// EditionList fetches a title's editions on expand.
function EditionList({ titleId }: { titleId: number }) {
  const [editions, setEditions] = useState<Edition[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .listEditions(titleId)
      .then((e) => !cancelled && setEditions(e))
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load editions'))
    return () => {
      cancelled = true
    }
  }, [titleId])

  if (err) return <p className="error">Edition error: {err}</p>
  if (!editions) return <p className="dim">Loading editions…</p>
  if (editions.length === 0) return <p className="dim">No editions yet.</p>
  return (
    <div className="child-list nested" data-testid={`editions-${titleId}`}>
      {editions.map((ed) => (
        <div className="child-row" key={ed.ID}>
          <span className="child-label">{ed.Format || '—'}</span>
          <span className="lt-name">{ed.ISBN ? `ISBN ${ed.ISBN}` : ed.Publisher || '—'}</span>
          <MonitoredToggle
            monitored={ed.Monitored}
            onToggle={async (next) => {
              await client.setEditionMonitored(ed.ID, next)
            }}
            label={`edition monitoring`}
            testId={`edition-mon-${ed.ID}`}
          />
        </div>
      ))}
    </div>
  )
}
