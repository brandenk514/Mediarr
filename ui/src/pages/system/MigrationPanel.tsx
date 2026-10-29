import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { MigrationInclude, MigrationRequest, MigrationResult, MigrationStatus } from '../../api/types'

// MigrationPanel is System > Import: the drop-in migration (P6: Sonarr;
// R6: + Radarr). Point it at a Sonarr or Radarr instance (base URL + API
// key), dry-run first, then run for real. The mapping report shows every
// entity's action (created / exists / updated / skipped / error / dry-run)
// and whether the run is resumable.
const ENTITIES: { key: keyof MigrationInclude; label: string }[] = [
  { key: 'series', label: 'series' },
  { key: 'episodes', label: 'episodes' },
  { key: 'quality_profiles', label: 'quality profiles' },
  { key: 'tags', label: 'tags' },
  { key: 'indexers', label: 'indexers' },
  { key: 'download_client', label: 'download client' },
  // Lidarr-only: import the source's root folders with their derived
  // per-root default profiles (L5). Ignored by the Sonarr/Radarr migrators.
  { key: 'root_folders', label: 'root folders' },
]

type MigrationAction = MigrationResult['items'][number]['action']

const ACTION_PILL: Record<MigrationAction, string> = {
  created: 'pill-ok',
  exists: 'pill-idle',
  updated: 'pill-adding',
  skipped: 'pill-unknown',
  error: 'pill-err',
  'dry-run': 'pill-warn',
}

const STATUS_PILL: Record<MigrationStatus['status'], string> = {
  idle: 'pill-idle',
  running: 'pill-downloading',
  completed: 'pill-ok',
  partial: 'pill-warn',
  failed: 'pill-err',
}

export function MigrationPanel() {
  const [status, setStatus] = useState<MigrationStatus | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [sourceUrl, setSourceUrl] = useState('')
  const [source, setSource] = useState<MigrationRequest['source']>('sonarr')
  const [apiKey, setApiKey] = useState('')
  const [dryRun, setDryRun] = useState(true)
  const [resume, setResume] = useState(false)
  const [include, setInclude] = useState<Required<MigrationInclude>>({
    series: true,
    episodes: true,
    quality_profiles: true,
    tags: true,
    indexers: true,
    download_client: true,
    root_folders: true,
  })

  const [running, setRunning] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const [result, setResult] = useState<MigrationResult | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setStatus(await client.getMigrationStatus())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load migration status')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error) {
    return (
      <div className="card" data-testid="system-migration-error">
        <h2>Migration unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (!status) {
    return (
      <div className="card" data-testid="system-migration">
        <h3 style={{ margin: '0 0 0.5rem' }}>Migration</h3>
        <p className="dim">Loading migration status…</p>
      </div>
    )
  }

  async function handleRun() {
    setRunning(true)
    setRunError(null)
    try {
      setResult(
        await client.runMigration({
          source_url: sourceUrl.trim(),
          source,
          api_key: apiKey.trim() || undefined,
          dry_run: dryRun,
          resume: resume || undefined,
          include,
        }),
      )
    } catch (e) {
      setRunError(e instanceof Error ? e.message : 'migration run failed')
    } finally {
      setRunning(false)
    }
  }

  const report = result ?? (status.last_result as MigrationResult | null | undefined) ?? null

  return (
    <div data-testid="system-migration">
      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Migration</h3>
        <p className="dim" style={{ marginTop: 0 }}>
          {source === 'lidarr'
            ? 'Drop-in migration from a Lidarr instance: artists + albums (type, monitoring, tracks), quality profiles + definitions, metadata profiles, tags, root folders (with default profiles), indexers, and the download client.'
            : source === 'radarr'
              ? 'Drop-in migration from a Radarr instance: movies + editions, quality profiles, tags, indexers, the download client, and a best-effort settings subset.'
              : 'Drop-in migration from a Sonarr instance: series + episodes, quality profiles, tags, indexers, and the download client.'}{' '}
          Idempotent and resumable — a dry run changes nothing.
        </p>
        <div className="list-table" data-testid="migration-status">
          <div className="lt-row">
            <span>
              <span className={`pill ${STATUS_PILL[status.status]}`}>{status.status}</span>
            </span>
            <span className="lt-dim">
              {status.done}/{status.total} {source === 'lidarr' ? 'artists' : source === 'radarr' ? 'movies' : 'series'}
            </span>
            <span className="lt-dim">
              {status.started_at ? `started ${new Date(status.started_at).toLocaleString()}` : ''}
              {status.finished_at ? ` · finished ${new Date(status.finished_at).toLocaleString()}` : ''}
            </span>
          </div>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Source</h3>
        <label>
          Source application
          <select
            data-testid="migration-source"
            value={source}
            onChange={(e) => setSource(e.target.value as MigrationRequest['source'])}
          >
            <option value="sonarr">Sonarr</option>
            <option value="radarr">Radarr</option>
            <option value="lidarr">Lidarr</option>
          </select>
        </label>
        <p className="dim" data-testid="migration-source-help" style={{ marginTop: 0 }}>
          {source === 'lidarr'
            ? 'Base URL of the Lidarr instance.'
            : source === 'radarr'
              ? 'Base URL of the Radarr instance.'
              : 'Base URL of the Sonarr instance.'}
        </p>
        <label>
          {source === 'lidarr' ? 'Lidarr base URL' : source === 'radarr' ? 'Radarr base URL' : 'Sonarr base URL'}
          <input
            data-testid="migration-source-url"
            type="url"
            placeholder={source === 'lidarr' ? 'https://lidarr.local:8666' : source === 'radarr' ? 'https://radarr.local:7878' : 'http://sonarr.local:8989'}
            value={sourceUrl}
            onChange={(e) => setSourceUrl(e.target.value)}
          />
        </label>
        <label>
          {source === 'lidarr' ? 'Lidarr API key' : source === 'radarr' ? 'Radarr API key' : 'Sonarr API key'}
          <input
            data-testid="migration-api-key"
            type="password"
            placeholder="X-Api-Key (optional for LAN instances without auth)"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
        </label>
        <label className="dim">
          <input
            type="checkbox"
            data-testid="migration-dry-run"
            checked={dryRun}
            onChange={(e) => setDryRun(e.target.checked)}
          />{' '}
          dry run — report only, add nothing
        </label>
        <label className="dim">
          <input
            type="checkbox"
            data-testid="migration-resume"
            checked={resume}
            onChange={(e) => setResume(e.target.checked)}
          />{' '}
          resume a partial run (skip already-migrated series)
        </label>
        <div className="dim" style={{ marginTop: '0.8rem' }}>
          Include
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.25rem 1.25rem' }}>
          {ENTITIES.filter((e) => e.key !== 'root_folders' || source === 'lidarr').map(({ key, label }) => (
            <label key={key} className="dim" style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', marginTop: 0 }}>
              <input
                type="checkbox"
                data-testid={`migration-include-${key}`}
                checked={include[key]}
                onChange={(e) => setInclude((inc) => ({ ...inc, [key]: e.target.checked }))}
              />{' '}
              {label}
            </label>
          ))}
        </div>
        <button
          data-testid="migration-run-btn"
          disabled={running || !sourceUrl.trim()}
          onClick={handleRun}
        >
          {running ? 'Running…' : dryRun ? 'Dry run' : 'Run migration'}
        </button>
        {runError && <p className="error">{runError}</p>}
      </div>

      {report && (
        <div className="card" data-testid="migration-result">
          <h3 style={{ margin: '0 0 0.5rem' }}>
            Mapping report{report.dry_run ? ' (dry run)' : ''}
          </h3>
          <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }} data-testid="migration-counts">
            <span className="dim small">
              total <strong>{report.counts.total}</strong>
            </span>
            <span className="dim small">
              created <strong>{report.counts.created}</strong>
            </span>
            <span className="dim small">
              existing <strong>{report.counts.existing}</strong>
            </span>
            <span className="dim small">
              skipped <strong>{report.counts.skipped}</strong>
            </span>
            <span className="dim small">
              errors <strong>{report.counts.errors}</strong>
            </span>
          </div>
          {report.resumable && (
            <div className="banner-success" data-testid="migration-resumable">
              This run was partial — enable “resume” and run again to continue.
            </div>
          )}
          {report.items.length > 0 && (
            <div className="list-table">
              <div className="lt-head">
                <span>Entity</span>
                <span>Source</span>
                <span>Action</span>
                <span>Target id</span>
                <span>Message</span>
              </div>
              {report.items.map((item, i) => (
                <div
                  className="lt-row"
                  data-testid={`migration-item-${i}`}
                  key={`${item.entity}-${item.external_id}-${i}`}
                >
                  <span>{item.entity}</span>
                  <span className="lt-dim">{item.source_title || item.external_id}</span>
                  <span>
                    <span className={`pill ${ACTION_PILL[item.action]}`}>{item.action}</span>
                  </span>
                  <span>{item.target_id ? `#${item.target_id}` : '—'}</span>
                  <span className="lt-dim">{item.message ?? ''}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
