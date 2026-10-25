import { useCallback, useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { client } from '../../api/client'
import type { PageWantedItem, WantedEpisodeRef, WantedItem, WantedTab } from '../../api/types'
import { DEFAULT_PAGE_SIZE, Pagination } from '../../components/list'
import { EmptyState, Modal, TbButton, Toolbar } from '../../components/ui'

// Wanted is the Missing / Cutoff Unmet page (route established in P1). P3:
// real server-side lists with Search All, Unmonitor Selected, Manual Import,
// and server-side pagination, backed by the global wanted endpoints.
export function WantedPage({ tab }: { tab: WantedTab }) {
  const isMissing = tab === 'missing'

  const [data, setData] = useState<PageWantedItem | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState<string | null>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [importTarget, setImportTarget] = useState<WantedItem | null>(null)
  const [filePath, setFilePath] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const p = await client.listWanted({ tab, q: q || undefined, page, pageSize: DEFAULT_PAGE_SIZE })
      setData(p)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load wanted')
    } finally {
      setLoading(false)
    }
  }, [tab, q, page])

  useEffect(() => {
    void load()
  }, [load])

  // Reset selection + search + page when the tab changes.
  useEffect(() => {
    setSelected(new Set())
    setPage(1)
    setText('')
    setQ('')
  }, [tab])

  // Debounce the search box into q, resetting the page.
  useEffect(() => {
    const t = setTimeout(() => {
      setQ(text.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [text])

  function rowKey(w: WantedItem): string {
    return `${w.series_id}:${w.season}:${w.episode}`
  }
  function toggle(w: WantedItem) {
    const k = rowKey(w)
    setSelected((prev) => {
      const n = new Set(prev)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  }
  function toggleAll() {
    if (!data) return
    setSelected((prev) => {
      const all = data.items.length > 0 && data.items.every((w) => prev.has(rowKey(w)))
      const n = new Set(prev)
      for (const w of data.items) {
        if (all) n.delete(rowKey(w))
        else n.add(rowKey(w))
      }
      return n
    })
  }
  function selectedRefs(): WantedEpisodeRef[] {
    if (!data) return []
    return data.items
      .filter((w) => selected.has(rowKey(w)))
      .map((w) => ({ series_id: w.series_id, season: w.season, episode: w.episode }))
  }

  async function searchAll() {
    setBusy('search')
    setMsg(null)
    try {
      const r = await client.searchWanted(tab)
      const n = r.searched ?? 0
      setMsg(`Search requested for ${n} ${isMissing ? 'missing' : 'cutoff-unmet'} episode${n === 1 ? '' : 's'}.`)
      void load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'search failed')
    } finally {
      setBusy(null)
    }
  }

  async function unmonitorSelected() {
    const refs = selectedRefs()
    if (refs.length === 0) return
    setBusy('unmonitor')
    setMsg(null)
    try {
      const r = await client.unmonitorWanted(refs)
      const n = r.unmonitored ?? 0
      setMsg(`Unmonitored ${n} episode${n === 1 ? '' : 's'}.`)
      setSelected(new Set())
      void load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'unmonitor failed')
    } finally {
      setBusy(null)
    }
  }

  function openImport(w: WantedItem) {
    setImportTarget(w)
    setFilePath('')
  }

  async function doImport() {
    if (!importTarget) return
    setBusy('import')
    try {
      const r = await client.manualImport({
        kind: 'tv',
        series_id: importTarget.series_id,
        season: importTarget.season,
        episode: importTarget.episode,
        file_path: filePath,
      })
      setMsg(r.message)
      setImportTarget(null)
      setFilePath('')
      void load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'import failed')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="list-page" data-testid={`wanted-${tab}`}>
      <div className="page-head">
        <h2>Wanted</h2>
      </div>
      <div className="segmented" role="tablist" aria-label="Wanted" style={{ marginBottom: '1rem' }}>
        <NavLink to="/wanted/missing" role="tab" aria-selected={isMissing} className={`seg ${isMissing ? 'active' : ''}`} data-testid="wanted-tab-missing">
          Missing
        </NavLink>
        <NavLink to="/wanted/cutoff" role="tab" aria-selected={!isMissing} className={`seg ${!isMissing ? 'active' : ''}`} data-testid="wanted-tab-cutoff">
          Cutoff Unmet
        </NavLink>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="🔍" label="Search All" testId={`wanted-search-${tab}`} onClick={() => void searchAll()} disabled={busy !== null} />
            <TbButton icon="👁" label="Unmonitor Selected" testId={`wanted-unmonitor-${tab}`} onClick={() => void unmonitorSelected()} disabled={selected.size === 0 || busy !== null} divider />
            <TbButton icon="📥" label="Manual Import" testId={`wanted-manual-${tab}`} onClick={() => { const refs = selectedRefs(); if (data && refs.length === 1) openImport(data.items.find((w) => rowKey(w) === refs[0].series_id + ':' + refs[0].season + ':' + refs[0].episode)!) }} disabled={selected.size !== 1 || busy !== null} />
          </>
        }
        right={
          <input
            className="list-search"
            type="search"
            placeholder="Filter by series…"
            value={text}
            onChange={(e) => setText(e.target.value)}
            data-testid={`wanted-filter-${tab}`}
            aria-label="Filter wanted by series"
            style={{ width: 200 }}
          />
        }
      />
      {msg && (
        <p className="dim small" role="status" data-testid={`wanted-msg-${tab}`}>
          {msg}
        </p>
      )}
      {error ? (
        <p className="error small" data-testid={`wanted-error-${tab}`}>
          {error}
        </p>
      ) : loading && !data ? (
        <p className="dim">Loading wanted…</p>
      ) : !data || data.items.length === 0 ? (
        <EmptyState
          title={isMissing ? 'No missing episodes' : 'No cutoff-unmet episodes'}
          message={
            isMissing
              ? 'Monitored episodes with no file appear here.'
              : 'Episodes whose current file is below their quality profile’s cutoff appear here.'
          }
          testId={`wanted-empty-${tab}`}
        />
      ) : (
        <div className="list-table" data-testid={`wanted-table-${tab}`}>
          <div className="lt-head">
            <input type="checkbox" aria-label="Select all" data-testid={`wanted-select-all-${tab}`} checked={data.items.length > 0 && data.items.every((w) => selected.has(rowKey(w)))} onChange={() => toggleAll()} />
            <span>Series</span>
            <span>Episode</span>
            <span>Title</span>
            <span>Air date</span>
            <span>Status</span>
            <span />
          </div>
          {data.items.map((w) => {
            const k = rowKey(w)
            return (
              <div key={k} className="lt-row" data-testid={`wanted-row-${w.series_id}-${w.season}-${w.episode}`}>
                <input type="checkbox" aria-label={`Select ${w.series_title} S${w.season}E${w.episode}`} checked={selected.has(k)} onChange={() => toggle(w)} data-testid={`wanted-check-${w.series_id}-${w.season}-${w.episode}`} />
                <span className="lt-name">{w.series_title}</span>
                <span className="lt-dim">S{w.season}E{w.episode}</span>
                <span className="lt-dim">{w.episode_title || '—'}</span>
                <span className="lt-dim">{w.air_date || '—'}</span>
                <span className="lt-dim" data-testid={`wanted-status-${w.series_id}-${w.season}-${w.episode}`}>{w.status}</span>
                <button className="ghost" onClick={() => openImport(w)} data-testid={`wanted-import-${w.series_id}-${w.season}-${w.episode}`}>
                  Import
                </button>
              </div>
            )
          })}
        </div>
      )}
      <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={data?.total ?? 0} onPage={(p) => setPage(p)} />

      {importTarget && (
        <Modal title={`Import ${importTarget.series_title} S${importTarget.season}E${importTarget.episode}`} onClose={() => setImportTarget(null)} testId="wanted-import-modal" footer={
          <>
            <button className="ghost" onClick={() => setImportTarget(null)} data-testid="wanted-import-cancel">
              Cancel
            </button>
            <button disabled={busy !== null || !filePath} onClick={() => void doImport()} data-testid="wanted-import-submit">
              Import
            </button>
          </>
        }>
          <p className="dim small">
            Enter the server-side path of the episode file to import.
          </p>
          <input
            className="list-search"
            style={{ width: '100%' }}
            value={filePath}
            onChange={(e) => setFilePath(e.target.value)}
            placeholder="/media/tv/<Series>/<file>.mkv"
            data-testid="wanted-import-path"
            aria-label="File path to import"
          />
        </Modal>
      )}
    </div>
  )
}
