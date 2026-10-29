import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { StatsDayBucket, StatsIndexerSeries, StatsSummary } from '../../api/types'
import { EmptyState } from '../../components/ui'

// IndexerStatsPage is the Prowlarr PR3 Stats page (plan §2 "Stats"): a
// Refresh toolbar, four KPI cards (Active Indexers · Total Queries · Total
// Grabs · Active Apps), two large time-series charts over the 30-day series,
// and one small chart per active indexer (series.by_indexer). There is no
// charting library in the UI — the charts are lightweight inline SVG computed
// client-side from the buckets (all 30 UTC-day buckets are present, zero days
// included, so the x-axis is stable).
export function IndexerStatsPage() {
  const [data, setData] = useState<StatsSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const s = await client.indexerStatsSummary()
      setData(s)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load indexer stats')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // Guard the load boundary: the wire shape makes series/by_indexer required,
  // but an empty or partial document must still render (no .map on undefined).
  const queries = data?.series?.queries ?? []
  const grabs = data?.series?.grabs ?? []
  const byIndexer = data?.series?.by_indexer ?? []

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="indexers-stats-title">Indexer Stats</h2>
        <div className="dash-head-right">
          <button
            className="ghost"
            onClick={() => void load()}
            disabled={loading}
            data-testid="indexers-stats-refresh"
          >
            Refresh
          </button>
        </div>
      </div>

      {error && !data ? (
        <div className="card" data-testid="indexers-stats-error">
          <h3>Stats unavailable</h3>
          <p className="error" data-testid="indexers-stats-error-message">
            {error}
          </p>
          <button onClick={() => void load()}>Retry</button>
        </div>
      ) : !data ? (
        <div className="card" data-testid="indexers-stats-loading">
          <p className="dim">Loading stats…</p>
        </div>
      ) : (
        <>
          <div className="kpi-grid" data-testid="indexers-stats-kpis">
            <KpiCard label="Active Indexers" value={data.active_indexers} testId="kpi-active-indexers" />
            <KpiCard label="Total Queries" value={data.total_queries} testId="kpi-total-queries" />
            <KpiCard label="Total Grabs" value={data.total_grabs} testId="kpi-total-grabs" />
            <KpiCard label="Active Apps" value={data.active_apps} testId="kpi-active-apps" />
          </div>

          <div className="stats-charts">
            <div className="card" data-testid="indexers-stats-queries-chart">
              <h3>Queries — last 30 days</h3>
              <TimeChart buckets={queries} testId="chart-queries" />
            </div>
            <div className="card" data-testid="indexers-stats-grabs-chart">
              <h3>Grabs — last 30 days</h3>
              <TimeChart buckets={grabs} testId="chart-grabs" />
            </div>
          </div>

          {byIndexer.length > 0 ? (
            <div className="stats-by-indexer" data-testid="indexers-stats-by-indexer">
              {byIndexer.map((s) => (
                <div
                  key={s.indexer}
                  className="card"
                  data-testid={`indexers-stats-indexer-${slug(s.indexer)}`}
                >
                  <h3>{s.indexer}</h3>
                  <IndexerChart series={s} />
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              title="No indexer activity"
              message="No indexer has queries or grabs in the last 30 days."
              testId="indexers-stats-no-indexers"
            />
          )}
        </>
      )}
    </div>
  )
}

// ---- KPI cards ----

function KpiCard({ label, value, testId }: { label: string; value: number; testId: string }) {
  return (
    <div className="card kpi-card" data-testid={testId}>
      <span className="kpi-value" data-testid={`${testId}-value`}>
        {fmtBig(value)}
      </span>
      <span className="kpi-label">{label}</span>
    </div>
  )
}

// fmtBig renders large numbers the way Prowlarr's KPI cards do (49400 -> 49.4K).
function fmtBig(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

// ---- inline SVG charts (no charting dependency) ----

const W = 360
const H = 110
const PAD = { l: 8, r: 8, t: 8, b: 20 }

function linePoints(buckets: StatsDayBucket[], which: 'count'): [number, number][] {
  const n = buckets.length
  const innerW = W - PAD.l - PAD.r
  const innerH = H - PAD.t - PAD.b
  const max = Math.max(1, ...buckets.map((b) => b[which]))
  return buckets.map((b, i) => {
    const x = n <= 1 ? PAD.l + innerW / 2 : PAD.l + (i * innerW) / (n - 1)
    const y = PAD.t + innerH - (b[which] / max) * innerH
    return [x, y]
  })
}

function toPointsAttr(pts: [number, number][]): string {
  return pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
}

function axisLabels(buckets: StatsDayBucket[]) {
  if (buckets.length === 0) return { first: '', last: '' }
  return { first: buckets[0].date.slice(5), last: buckets[buckets.length - 1].date.slice(5) }
}

// TimeChart is one area/line chart over a 30-day bucket series (queries or
// grabs). Empty series render a placeholder instead of an empty SVG.
export function TimeChart({ buckets, testId, tone = 'accent' }: { buckets: StatsDayBucket[]; testId?: string; tone?: 'accent' | 'warn' }) {
  if (!buckets.length) {
    return <p className="dim small" data-testid={testId ? `${testId}-empty` : undefined}>No data</p>
  }
  const pts = linePoints(buckets, 'count')
  const bottom = H - PAD.b
  const area = `${PAD.l},${bottom} ${toPointsAttr(pts)} ${pts[pts.length - 1][0].toFixed(1)},${bottom}`
  const axis = axisLabels(buckets)
  return (
    <svg
      className="ts-svg"
      viewBox={`0 0 ${W} ${H}`}
      role="img"
      aria-label="time series chart"
      data-testid={testId}
    >
      <polygon className={tone === 'warn' ? 'ts-area ts-area-warn' : 'ts-area'} points={area} />
      <polyline className={tone === 'warn' ? 'ts-line ts-line-warn' : 'ts-line'} points={toPointsAttr(pts)} />
      <text x={PAD.l} y={H - 6} className="ts-axis">
        {axis.first}
      </text>
      <text x={W - PAD.r} y={H - 6} className="ts-axis" textAnchor="end">
        {axis.last}
      </text>
    </svg>
  )
}

// IndexerChart is the per-active-indexer small chart: queries (teal) and grabs
// (orange) as two lines over the same 30-day x-axis, with a legend.
export function IndexerChart({ series }: { series: StatsIndexerSeries }) {
  if (!series.queries.length && !series.grabs.length) {
    return <p className="dim small">No data</p>
  }
  // Both series share the full 30-day bucket set, so one x-scale works for both.
  const qPts = series.queries.length ? linePoints(series.queries, 'count') : []
  const gPts = series.grabs.length ? linePoints(series.grabs, 'count') : []
  const axis = axisLabels(series.queries.length ? series.queries : series.grabs)
  return (
    <div className="indexer-chart">
      <svg className="ts-svg" viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${series.indexer} activity`}>
        {qPts.length > 1 && (
          <polygon className="ts-area" points={`${PAD.l},${H - PAD.b} ${toPointsAttr(qPts)} ${qPts[qPts.length - 1][0].toFixed(1)},${H - PAD.b}`} />
        )}
        {qPts.length > 0 && <polyline className="ts-line" points={toPointsAttr(qPts)} />}
        {gPts.length > 0 && <polyline className="ts-line ts-line-warn" points={toPointsAttr(gPts)} />}
        <text x={PAD.l} y={H - 6} className="ts-axis">
          {axis.first}
        </text>
        <text x={W - PAD.r} y={H - 6} className="ts-axis" textAnchor="end">
          {axis.last}
        </text>
      </svg>
      <div className="stats-legend">
        <span>
          <span className="sw sw-queries" aria-hidden /> Queries
        </span>
        <span>
          <span className="sw sw-grabs" aria-hidden /> Grabs
        </span>
      </div>
    </div>
  )
}

// slug maps an indexer name to a stable test-id fragment (nyaa -> nyaa).
function slug(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-')
}
