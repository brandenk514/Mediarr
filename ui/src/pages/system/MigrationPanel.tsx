import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type {
  MigrationInclude,
  MigrationRequest,
  MigrationResult,
  MigrationStatus,
  ReadarrMigrationRequest,
} from '../../api/types'

// MigrationPanel is System > Import: the drop-in migration (P6: Sonarr;
// R6: + Radarr; L5: + Lidarr; PR6: + Prowlarr; B6: + Readarr). Point it at a
// live instance (base URL + API key), dry-run first, then run for real. The
// mapping report shows every entity's action (created / exists / updated /
// skipped / error / dry-run) and whether the run is resumable.
//
// Readarr (B6) is driven through the dedicated /system/migration/readarr
// endpoints: a fixed dry-run (read-only), an explicit execute (with a
// confirm — it writes for real), and a source-scoped status that reports the
// Readarr run even when another source migrated most recently. The other
// sources keep the P6 generic endpoint + most-recently-run status.
const ENTITIES: { key: keyof MigrationInclude; label: string }[] = [
  { key: 'series', label: 'series' },
  { key: 'episodes', label: 'episodes' },
  { key: 'quality_profiles', label: 'quality profiles' },
  { key: 'tags', label: 'tags' },
  { key: 'indexers', label: 'indexers' },
  { key: 'download_client', label: 'download client' },
  // Lidarr-only: import the source's root folders with their derived
  // per-root default profiles (L5). Readarr (B6) imports them too — with
  // the Calibre block. Ignored by the Sonarr/Radarr migrators.
  { key: 'root_folders', label: 'root folders' },
  // Prowlarr-only: the indexer proxies (FlareSolverr endpoints, PR6).
  // Ignored by the Sonarr/Radarr/Lidarr/Readarr migrators.
  { key: 'indexer_proxies', label: 'indexer proxies' },
]

// B6: the shared include keys map onto Readarr's own entity classes —
// series -> authors + books, episodes -> editions + book files, and the
// quality_profiles flag covers both book quality and metadata profiles.
const READARR_LABELS: Partial<Record<keyof MigrationInclude, string>> = {
  series: 'authors + books',
  episodes: 'editions + book files',
  quality_profiles: 'quality + metadata profiles',
  root_folders: 'root folders (Calibre block)',
}

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

// Per-source copy: labels, the done/total noun, the include filter, and the
// include labels (readarr re-labels the shared keys, see READARR_LABELS).
const SOURCE_COPY: Record<
  MigrationRequest['source'],
  {
    name: string
    blurb: string
    placeholder: string
    unit: string
    include: (key: keyof MigrationInclude) => boolean
  }
> = {
  sonarr: {
    name: 'Sonarr',
    blurb:
      'Drop-in migration from a Sonarr instance: series + episodes, quality profiles, tags, indexers, and the download client.',
    placeholder: 'http://sonarr.local:8989',
    unit: 'series',
    include: (key) => key !== 'root_folders' && key !== 'indexer_proxies',
  },
  radarr: {
    name: 'Radarr',
    blurb:
      'Drop-in migration from a Radarr instance: movies + editions, quality profiles, tags, indexers, the download client, and a best-effort settings subset.',
    placeholder: 'https://radarr.local:7878',
    unit: 'movies',
    include: (key) => key !== 'root_folders' && key !== 'indexer_proxies',
  },
  lidarr: {
    name: 'Lidarr',
    blurb:
      'Drop-in migration from a Lidarr instance: artists + albums (type, monitoring, tracks), quality profiles + definitions, metadata profiles, tags, root folders (with default profiles), indexers, and the download client.',
    placeholder: 'https://lidarr.local:8666',
    unit: 'artists',
    include: (key) => key !== 'indexer_proxies',
  },
  prowlarr: {
    name: 'Prowlarr',
    blurb:
      'Drop-in migration from a Prowlarr instance: indexers (credentials are re-encrypted at rest), download clients, tags, and indexer proxies. The Prowlarr Apps section is not imported — Mediarr replaces it with per-kind settings.',
    placeholder: 'https://prowlarr.local:9696',
    unit: 'entities',
    // Prowlarr (PR6) migrates exactly four entity classes — indexers,
    // download clients, tags, indexer proxies. No root folders, no apps.
    include: (key) =>
      key === 'indexers' ||
      key === 'download_client' ||
      key === 'tags' ||
      key === 'indexer_proxies',
  },
  readarr: {
    name: 'Readarr',
    blurb:
      'Drop-in migration from a Readarr instance: authors + books (editions, book files), quality + metadata profiles, tags, indexers, the download client, and root folders (with the Calibre block).',
    placeholder: 'http://readarr.local:8888',
    unit: 'books',
    // The Readarr migrator (B6) pulls the shared include classes — no
    // indexer proxies. Root folders come with the Calibre block.
    include: (key) => key !== 'indexer_proxies',
  },
}

// includeLabel resolves the display label for an include checkbox: the
// source-specific override (readarr) or the shared entity label.
function includeLabel(source: MigrationRequest['source'], key: keyof MigrationInclude): string {
  if (source === 'readarr') {
    return READARR_LABELS[key] ?? ENTITIES.find((e) => e.key === key)?.label ?? key
  }
  return ENTITIES.find((e) => e.key === key)?.label ?? key
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
    indexer_proxies: true,
  })

  const [running, setRunning] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const [result, setResult] = useState<MigrationResult | null>(null)

  const copy = SOURCE_COPY[source]
  const isReadarr = source === 'readarr'

  // B6: Readarr's status is source-scoped (the dedicated endpoint), so it
  // reports the Readarr run even when another source migrated most recently;
  // the other sources share the generic most-recently-run status.
  const load = useCallback(async () => {
    setError(null)
    try {
      setStatus(
        source === 'readarr'
          ? await client.getReadarrMigrationStatus()
          : await client.getMigrationStatus(),
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load migration status')
    }
  }, [source])

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
    // B6: the dedicated execute writes for real — confirm before running.
    // (Dry runs are read-only and safe to repeat, so they need no confirm.)
    if (!dryRun && isReadarr) {
      const ok = window.confirm(
        'Run the Readarr migration for real? Authors, books, and settings will be imported into Mediarr. A dry run changes nothing.',
      )
      if (!ok) return
    }
    setRunning(true)
    setRunError(null)
    try {
      if (isReadarr) {
        const req: ReadarrMigrationRequest = {
          base_url: sourceUrl.trim(),
          api_key: apiKey.trim() || undefined,
          include,
        }
        setResult(
          dryRun
            ? await client.runReadarrMigration(req)
            : await client.executeReadarrMigration({ ...req, resume: resume || undefined }),
        )
      } else {
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
      }
    } catch (e) {
      setRunError(e instanceof Error ? e.message : 'migration run failed')
    } finally {
      setRunning(false)
      void load()
    }
  }

  const report = result ?? (status.last_result as MigrationResult | null | undefined) ?? null

  return (
    <div data-testid="system-migration">
      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>Migration</h3>
        <p className="dim" style={{ marginTop: 0 }}>
          {copy.blurb}{' '}
          Idempotent and resumable — a dry run changes nothing.
        </p>
        <div className="list-table" data-testid="migration-status">
          <div className="lt-row">
            <span>
              <span className={`pill ${STATUS_PILL[status.status]}`}>{status.status}</span>
            </span>
            <span className="lt-dim">
              {status.done}/{status.total} {copy.unit}
            </span>
            <span className="lt-dim">
              {status.started_at ? `started ${new Date(status.started_at).toLocaleString()}` : ''}
              {status.finished_at ? ` · finished ${new Date(status.finished_at).toLocaleString()}` : ''}
            </span>
          </div>
        </div>
        {status.status === 'failed' && (
          <div className="banner-danger" data-testid="migration-failed">
            The last run failed — the source could not be reached or returned no data. Check the
            base URL and API key, then try again.
          </div>
        )}
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
            <option value="prowlarr">Prowlarr</option>
            <option value="readarr">Readarr</option>
          </select>
        </label>
        <p className="dim" data-testid="migration-source-help" style={{ marginTop: 0 }}>
          Base URL of the {copy.name} instance.
        </p>
        <label>
          {copy.name} base URL
          <input
            data-testid="migration-source-url"
            type="url"
            placeholder={copy.placeholder}
            value={sourceUrl}
            onChange={(e) => setSourceUrl(e.target.value)}
          />
        </label>
        <label>
          {copy.name} API key
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
          resume a partial run (skip already-migrated {copy.unit})
        </label>
        <div className="dim" style={{ marginTop: '0.8rem' }}>
          Include
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.25rem 1.25rem' }}>
          {ENTITIES.filter((e) => copy.include(e.key)).map(({ key }) => (
            <label
              key={key}
              className="dim"
              style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', marginTop: 0 }}
            >
              <input
                type="checkbox"
                data-testid={`migration-include-${key}`}
                checked={include[key]}
                onChange={(e) => setInclude((inc) => ({ ...inc, [key]: e.target.checked }))}
              />{' '}
              {includeLabel(source, key)}
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
          {report.items.length > 0 ? (
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
          ) : (
            // The unreachable-source contract: a 200 with an empty report
            // (status 'failed'). Nothing was imported.
            <p className="dim" data-testid="migration-empty-report">
              No entities were mapped. If the source is unreachable or empty, check the base URL
              and API key, then run again.
            </p>
          )}
        </div>
      )}
    </div>
  )
}
