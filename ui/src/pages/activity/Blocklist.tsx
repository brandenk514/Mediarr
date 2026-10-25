import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { BlocklistEntry, MediaKind } from '../../api/types'
import { EmptyState, TbButton, Toolbar } from '../../components/ui'
import { timeAgo } from '../../lib/format'

const KINDS: MediaKind[] = ['tv', 'movie', 'music', 'book']

// Blocklist is the blocked (rejected) releases list at /activity/blocklist
// (route established in P1). P3: real list + refresh + kind filter + per-row
// unblock + reason, backed by the blocklist endpoints.
export function BlocklistPage() {
  const [items, setItems] = useState<BlocklistEntry[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [kind, setKind] = useState<MediaKind | ''>('')
  const [busyId, setBusyId] = useState<number | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const rows = await client.listBlocklist({ kind: kind || undefined })
      setItems(rows)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load blocklist')
    } finally {
      setLoading(false)
    }
  }, [kind])

  useEffect(() => {
    void load()
  }, [load])

  async function unblock(id: number) {
    setBusyId(id)
    try {
      await client.unblockRelease(id)
      setItems((prev) => (prev ? prev.filter((r) => r.id !== id) : prev))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to unblock release')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div className="list-page" data-testid="blocklist">
      <div className="page-head">
        <h2>Blocklist</h2>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh" testId="blocklist-refresh" onClick={() => void load()} />
            <select
              className="list-search"
              value={kind}
              onChange={(e) => setKind(e.target.value as MediaKind | '')}
              data-testid="blocklist-kind"
              aria-label="Filter by kind"
              style={{ width: 'auto' }}
            >
              <option value="">All kinds</option>
              {KINDS.map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </>
        }
      />
      {error ? (
        <p className="error small" data-testid="blocklist-error">
          {error}
        </p>
      ) : loading && !items ? (
        <p className="dim">Loading blocklist…</p>
      ) : !items || items.length === 0 ? (
        <EmptyState
          title="Blocklist is empty"
          message="Blocked (rejected) releases appear here. Unblock a release to let it be considered again."
          testId="blocklist-empty"
        />
      ) : (
        <div className="list-table" data-testid="blocklist-table">
          <div className="lt-head">
            <span>Release</span>
            <span>Media</span>
            <span>Indexer</span>
            <span>Reason</span>
            <span>Blocked</span>
            <span />
          </div>
          {items.map((r) => (
            <div key={r.id} className="lt-row" data-testid={`blocklist-row-${r.id}`}>
              <span className="lt-name" title={r.release_title}>
                {r.release_title}
              </span>
              <span className="lt-dim">{r.parent_name || r.kind}</span>
              <span className="lt-dim">{r.indexer || '—'}</span>
              <span className="lt-dim" data-testid={`blocklist-reason-${r.id}`}>
                {r.reason || '—'}
              </span>
              <span className="lt-dim">{timeAgo(r.created_at)}</span>
              <button
                className="ghost"
                data-testid={`blocklist-unblock-${r.id}`}
                disabled={busyId === r.id}
                onClick={() => void unblock(r.id)}
              >
                {busyId === r.id ? '…' : 'Unblock'}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
