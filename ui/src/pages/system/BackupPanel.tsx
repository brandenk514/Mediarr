import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { BackupDocument, BackupImportResult } from '../../api/types'

// BackupPanel is System > Backup: a read-back of the current self-contained
// config document (with optional queue/series snapshot toggles) plus export
// (JSON preview + download) and import (paste a previously exported
// document back in).
export function BackupPanel() {
  const [doc, setDoc] = useState<BackupDocument | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [includeQueue, setIncludeQueue] = useState(false)
  const [includeSeries, setIncludeSeries] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)
  const [exported, setExported] = useState<BackupDocument | null>(null)

  const [importText, setImportText] = useState('')
  const [importQueue, setImportQueue] = useState(false)
  const [importSeries, setImportSeries] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState<BackupImportResult | null>(null)
  const [importError, setImportError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setDoc(await client.getSystemBackup())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load backup')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error) {
    return (
      <div className="card" data-testid="system-backup-error">
        <h2>Backup unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (!doc) {
    return (
      <div className="card" data-testid="system-backup">
        <h3 style={{ margin: '0 0 0.5rem' }}>Backup</h3>
        <p className="dim">Loading current config…</p>
      </div>
    )
  }

  async function handleExport() {
    setExporting(true)
    setExportError(null)
    setExported(null)
    try {
      setExported(await client.getSystemBackup({ includeQueue, includeSeries }))
    } catch (e) {
      setExportError(e instanceof Error ? e.message : 'export failed')
    } finally {
      setExporting(false)
    }
  }

  function downloadExported() {
    if (!exported) return
    const blob = new Blob([JSON.stringify(exported, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `mediarr-backup-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  async function handleImport() {
    setImportError(null)
    setImportResult(null)
    let document: BackupDocument
    try {
      document = JSON.parse(importText) as BackupDocument
    } catch {
      setImportError('The pasted text is not valid JSON')
      return
    }
    setImporting(true)
    try {
      setImportResult(
        await client.putSystemBackup({
          document,
          include_queue: importQueue,
          include_series: importSeries,
        }),
      )
      setImportText('')
    } catch (e) {
      setImportError(e instanceof Error ? e.message : 'import failed')
    } finally {
      setImporting(false)
    }
  }

  const sectionCount = doc.settings?.sections?.length ?? 0
  const dlKind = doc.settings?.download_client?.kind ?? 'none'

  return (
    <div data-testid="system-backup">
      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Backup</h3>
        <p className="dim" style={{ marginTop: 0 }}>
          A self-contained export of this instance&apos;s configuration. Optional snapshots capture
          the queue and the series library.
        </p>
        <div className="list-table" style={{ marginTop: '0.75rem' }} data-testid="backup-summary">
          <div className="lt-row">
            <span>Sections</span>
            <span className="lt-dim">Download client</span>
            <span className="lt-dim">Document</span>
          </div>
          <div className="lt-row">
            <span>{sectionCount}</span>
            <span>
              <span className="pill">{dlKind}</span>
            </span>
            <span className="lt-dim">{doc.exported_at ? `v${doc.version} · ${new Date(doc.exported_at).toLocaleString()}` : '—'}</span>
          </div>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Export</h3>
        <label className="dim">
          <input
            type="checkbox"
            data-testid="backup-include-queue"
            checked={includeQueue}
            onChange={(e) => setIncludeQueue(e.target.checked)}
          />{' '}
          include queue snapshot
        </label>
        <label className="dim">
          <input
            type="checkbox"
            data-testid="backup-include-series"
            checked={includeSeries}
            onChange={(e) => setIncludeSeries(e.target.checked)}
          />{' '}
          include series snapshot
        </label>
        <button data-testid="backup-export-btn" disabled={exporting} onClick={handleExport}>
          {exporting ? 'Exporting…' : 'Export document'}
        </button>
        {exportError && <p className="error">{exportError}</p>}
        {exported && (
          <>
            <pre
              className="code"
              data-testid="backup-export-result"
              style={{ maxHeight: '24rem', overflow: 'auto' }}
            >
              {JSON.stringify(exported, null, 2)}
            </pre>
            <button className="ghost" data-testid="backup-download-btn" onClick={downloadExported}>
              Download .json
            </button>
          </>
        )}
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Import</h3>
        <p className="dim" style={{ marginTop: 0 }}>
          Paste a document exported from another (or this) instance. Settings always apply; the
          queue and series snapshots apply only when toggled.
        </p>
        <textarea
          data-testid="backup-import-text"
          rows={6}
          placeholder='{"version": "0.6.0", "settings": {"download_client": …}}'
          value={importText}
          onChange={(e) => setImportText(e.target.value)}
        />
        <label className="dim">
          <input
            type="checkbox"
            data-testid="backup-import-queue"
            checked={importQueue}
            onChange={(e) => setImportQueue(e.target.checked)}
          />{' '}
          apply queue snapshot
        </label>
        <label className="dim">
          <input
            type="checkbox"
            data-testid="backup-import-series"
            checked={importSeries}
            onChange={(e) => setImportSeries(e.target.checked)}
          />{' '}
          apply series snapshot
        </label>
        <button
          data-testid="backup-import-btn"
          disabled={importing || !importText.trim()}
          onClick={handleImport}
        >
          {importing ? 'Importing…' : 'Import document'}
        </button>
        {importError && <p className="error">{importError}</p>}
        {importResult && (
          <div className="banner-success" data-testid="backup-import-result">
            Imported {importResult.settings_imported} settings sections
            {importResult.queue_imported > 0 ? `, ${importResult.queue_imported} queue items` : ''}
            {importResult.series_imported > 0 ? `, ${importResult.series_imported} series` : ''}
            {importResult.message ? ` — ${importResult.message}` : ''}
          </div>
        )}
      </div>
    </div>
  )
}
