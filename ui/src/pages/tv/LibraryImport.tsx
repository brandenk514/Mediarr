import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { ImportReport, ImportCandidate, RootFolder } from '../../api/types'
import { Badge, Toolbar, TbButton } from '../../components/ui'

// LibraryImport is the Sonarr-parity Library Import wizard (P2, phase 1):
//   1. choose a root folder (registered, free/unmapped path)
//   2. scan -> the report's candidate folders with match status
//   3. confirm -> pick which matched folders to import
//   4. report -> summary
// Phase 1 ends at the report: actual file import/copy lands in a later phase,
// so the wizard never pretends to move files it hasn't.
type Step = 'root' | 'scan' | 'report'

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n
  let u = -1
  do {
    v /= 1024
    u += 1
  } while (v >= 1024 && u < units.length - 1)
  return `${v.toFixed(1)} ${units[u]}`
}

export function LibraryImportPage() {
  const [step, setStep] = useState<Step>('root')
  const [rootFolders, setRootFolders] = useState<RootFolder[] | null>(null)
  const [rootLoadError, setRootLoadError] = useState<string | null>(null)
  const [rootMode, setRootMode] = useState<'folder' | 'path'>('folder')
  const [rootId, setRootId] = useState<string>('')
  const [customPath, setCustomPath] = useState('')

  const [scanning, setScanning] = useState(false)
  const [scanError, setScanError] = useState<string | null>(null)
  const [report, setReport] = useState<ImportReport | null>(null)
  const [selected, setSelected] = useState<Set<string>>(() => new Set())

  const loadRootFolders = useCallback(() => {
    client
      .listRootFolders()
      .then((rfs) => {
        setRootFolders(rfs)
        if (rfs.length > 0) {
          setRootMode('folder')
          setRootId((cur) => cur || String(rfs[0]!.ID))
        } else {
          setRootMode('path')
        }
      })
      .catch((e: unknown) => setRootLoadError(e instanceof Error ? e.message : 'failed to load root folders'))
  }, [])

  useEffect(() => {
    loadRootFolders()
  }, [loadRootFolders])

  const canScan =
    (rootMode === 'folder' && rootId !== '') || (rootMode === 'path' && customPath.trim() !== '')

  async function scan() {
    setScanning(true)
    setScanError(null)
    setReport(null)
    try {
      const req: { root_folder_id?: number; path?: string } = {}
      if (rootMode === 'folder') req.root_folder_id = Number(rootId)
      else req.path = customPath.trim()
      const rep = await client.importLibrary(req)
      setReport(rep)
      // Preselect every matched candidate for import.
      setSelected(new Set(rep.Candidates.filter((c) => c.Matched).map((c) => c.Path)))
      setStep('scan')
    } catch (e) {
      // Advance anyway so the error line is visible (step 2 renders it).
      setStep('scan')
      setScanError(e instanceof Error ? e.message : 'scan failed')
    } finally {
      setScanning(false)
    }
  }

  function toggleCandidate(path: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  const matchedCount = report?.Candidates.filter((c) => c.Matched).length ?? 0

  return (
    <div className="list-page" data-testid="library-import">
      <div className="page-head">
        <h2>Library Import</h2>
      </div>

      <Toolbar
        left={
          <>
            {step !== 'root' && (
              <TbButton icon="←" label="Back" testId="import-back" onClick={() => { setStep('root'); setReport(null) }} />
            )}
            {step === 'root' && (
              <TbButton icon="🔍" label="Scan" testId="import-scan" disabled={!canScan || scanning} onClick={() => void scan()} />
            )}
            {step === 'scan' && (
              <TbButton icon="✓" label="Confirm" testId="import-confirm" onClick={() => setStep('report')} />
            )}
          </>
        }
        right={
          <ToolbarSteps step={step} />
        }
      />

      {step === 'root' && (
        <div className="card" data-testid="import-root">
          <p className="dim small">
            Choose a registered root folder, or enter an unmapped path to scan
            ad-hoc.
          </p>

          {rootLoadError && (
            <p className="dim small" data-testid="import-root-error">
              Root folders unavailable ({rootLoadError}) — use an unmapped path below.
            </p>
          )}
          {rootFolders === null && !rootLoadError && <p className="dim">Loading root folders…</p>}
          {rootFolders !== null && rootFolders.length > 0 && (
            <>
              <label className="check" htmlFor="import-mode-folder">
                <input
                  id="import-mode-folder"
                  type="radio"
                  name="import-mode"
                  checked={rootMode === 'folder'}
                  onChange={() => setRootMode('folder')}
                  data-testid="import-mode-folder"
                />
                Registered root folder
              </label>
              <select
                value={rootId}
                onChange={(e) => setRootId(e.target.value)}
                disabled={rootMode !== 'folder'}
                data-testid="import-root-select"
                aria-label="Root folder"
              >
                {rootFolders.map((rf) => (
                  <option key={rf.ID} value={String(rf.ID)}>
                    {rf.Name ? `${rf.Name} (${rf.Path})` : rf.Path}
                  </option>
                ))}
              </select>
            </>
          )}
          <label className="check" htmlFor="import-mode-path">
            <input
              id="import-mode-path"
              type="radio"
              name="import-mode"
              checked={rootMode === 'path'}
              onChange={() => setRootMode('path')}
              data-testid="import-mode-path"
            />
            Unmapped path
          </label>
          <input
            value={customPath}
            onChange={(e) => setCustomPath(e.target.value)}
            disabled={rootMode !== 'path'}
            placeholder="/absolute/path/to/tv"
            data-testid="import-path-input"
          />
          {rootFolders !== null && rootFolders.length === 0 && (
            <p className="dim small" data-testid="import-no-roots">
              No root folders registered — enter an unmapped path above.
            </p>
          )}
        </div>
      )}

      {step === 'scan' && (
        <>
          {scanning && <p className="dim" data-testid="import-scanning">Scanning…</p>}
          {scanError && (
            <p className="error" data-testid="import-scan-error">
              {scanError}
            </p>
          )}
          {report && (
            <div className="card" data-testid="import-results">
              <p className="dim small">
                Scanned <strong data-testid="import-scanned">{report.Scanned}</strong> candidate
                folder{report.Scanned === 1 ? '' : 's'} under {report.Root} —{' '}
                <strong data-testid="import-matched">{matchedCount}</strong> matched a tracked
                series. Select the folders to import:
              </p>
              {report.Candidates.length === 0 ? (
                <p className="dim" data-testid="import-no-candidates">
                  No series folders found in that root.
                </p>
              ) : (
                <table className="import-table" data-testid="import-candidates">
                  <thead>
                    <tr>
                      <th></th>
                      <th>Title</th>
                      <th>Year</th>
                      <th>Seasons</th>
                      <th>Files</th>
                      <th>Size</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.Candidates.map((c) => (
                      <CandidateRow
                        key={c.Path}
                        c={c}
                        checked={selected.has(c.Path)}
                        onToggle={() => toggleCandidate(c.Path)}
                      />
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </>
      )}

      {step === 'report' && report && (
        <div className="card" data-testid="import-report">
          <h3>Import report</h3>
          <p>
            Root: <strong>{report.Root}</strong>
          </p>
          <p>
            Candidate folders scanned: <strong data-testid="report-scanned">{report.Scanned}</strong>
          </p>
          <p>
            Matched series: <strong data-testid="report-matched">{matchedCount}</strong>
          </p>
          <p>
            Selected for import: <strong data-testid="report-selected">{selected.size}</strong>
          </p>
          <p className="dim small">
            Phase 1 ends here: file import (copy/move into the library) lands in
            a later phase.
          </p>
          <button className="ghost" onClick={() => { setStep('root'); setReport(null); setSelected(new Set()) }} data-testid="import-finish">
            Done
          </button>
        </div>
      )}
    </div>
  )
}

// ToolbarSteps renders the wizard's step indicator (Root folder / Scan /
// Report) in the toolbar's right group.
function ToolbarSteps({ step }: { step: Step }) {
  const steps: { key: Step; label: string }[] = [
    { key: 'root', label: 'Root folder' },
    { key: 'scan', label: 'Scan & match' },
    { key: 'report', label: 'Report' },
  ]
  const idx = steps.findIndex((s) => s.key === step)
  return (
    <div className="wizard-steps" data-testid="import-steps">
      {steps.map((s, i) => (
        <span
          key={s.key}
          className={`wizard-step ${i === idx ? 'active' : ''} ${i < idx ? 'done' : ''}`}
          data-testid={`import-step-${s.key}`}
        >
          {i + 1}. {s.label}
        </span>
      ))}
    </div>
  )
}

function CandidateRow({
  c,
  checked,
  onToggle,
}: {
  c: ImportCandidate
  checked: boolean
  onToggle: () => void
}) {
  return (
    <tr data-testid={`import-candidate-${c.Path}`}>
      <td>
        <input
          type="checkbox"
          checked={checked}
          onChange={onToggle}
          disabled={!c.Matched}
          aria-label={`Import ${c.Title}`}
          data-testid={`import-pick-${c.Path}`}
        />
      </td>
      <td>{c.Title}</td>
      <td>{c.Year > 0 ? c.Year : '—'}</td>
      <td>{c.Seasons}</td>
      <td>{c.Files}</td>
      <td>{formatBytes(c.Size)}</td>
      <td>
        {c.Matched ? (
          <Badge tone="ok" data-testid={`import-matched-${c.Path}`}>
            Matched (#{c.SeriesID})
          </Badge>
        ) : (
          <Badge data-testid={`import-unmatched-${c.Path}`}>
            Unmatched
          </Badge>
        )}
      </td>
    </tr>
  )
}
