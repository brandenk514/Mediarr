import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { StatsDayBucket, StatsIndexerSeries, StatsSummary } from '../../api/types'
import { IndexerStatsPage } from './IndexerStats'

// Mock the typed client so the stats page talks to an in-test fake.
vi.mock('../../api/client', () => ({
  client: {
    indexerStatsSummary: vi.fn(),
  },
}))

// bucketSeries builds a 30-day UTC-day series (zero days included) from a
// count pattern — the server always sends the full 30-bucket window.
function bucketSeries(counts: number[]): StatsDayBucket[] {
  const start = new Date('2026-09-29T00:00:00Z')
  return counts.map((count, i) => ({
    date: new Date(start.getTime() + i * 86_400_000).toISOString().slice(0, 10),
    count,
  }))
}

const DAILY = [1, 0, 2, 3, 0, 5, 4, 2, 6, 1, 3, 0, 2, 7, 5, 1, 4, 2, 8, 3, 0, 6, 2, 4, 9, 1, 3, 5, 2, 1]

// A seeded summary covering every render branch: four KPI totals, two
// 30-day series, and two by-indexer series (the per-indexer small charts).
function seedSummary(over: Partial<StatsSummary> = {}): StatsSummary {
  const byIndexer: StatsIndexerSeries[] = [
    { indexer: 'nyaa', queries: bucketSeries([2, 0, 1, ...DAILY.slice(3)]), grabs: bucketSeries([0, 1, 0, ...DAILY.slice(3)]) },
    { indexer: 'btsou', queries: bucketSeries([0, 0, 1, ...DAILY.slice(3)]), grabs: bucketSeries([0, 0, 0, ...DAILY.slice(3)]) },
  ]
  return {
    active_indexers: 5,
    active_apps: 0,
    total_queries: 49_400,
    total_grabs: 577,
    series: { queries: bucketSeries(DAILY), grabs: bucketSeries(DAILY), by_indexer: byIndexer },
    ...over,
  }
}

function renderStats() {
  return render(
    <MemoryRouter initialEntries={['/indexers/stats']}>,
      <IndexerStatsPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.indexerStatsSummary as ReturnType<typeof vi.fn>).mockResolvedValue(seedSummary())
})

describe('IndexerStatsPage (PR3 stats rollup)', () => {
  it('renders the four KPI cards with formatted values', async () => {
    renderStats()
    expect(screen.getByTestId('indexers-stats-title')).toHaveTextContent('Indexer Stats')
    await screen.findByTestId('indexers-stats-kpis')
    expect(screen.getByTestId('kpi-active-indexers-value')).toHaveTextContent('5')
    expect(screen.getByTestId('kpi-total-queries-value')).toHaveTextContent('49.4K')
    expect(screen.getByTestId('kpi-total-grabs-value')).toHaveTextContent('577')
    expect(screen.getByTestId('kpi-active-apps-value')).toHaveTextContent('0')
    // Labels.
    expect(screen.getByTestId('kpi-active-indexers')).toHaveTextContent('Active Indexers')
    expect(screen.getByTestId('kpi-total-queries')).toHaveTextContent('Total Queries')
    expect(screen.getByTestId('kpi-total-grabs')).toHaveTextContent('Total Grabs')
    expect(screen.getByTestId('kpi-active-apps')).toHaveTextContent('Active Apps')
  })

  it('renders the two large time-series charts from the seeded summary', async () => {
    renderStats()
    await screen.findByTestId('chart-queries')
    const q = screen.getByTestId('chart-queries')
    const g = screen.getByTestId('chart-grabs')
    expect(q.tagName.toLowerCase()).toBe('svg')
    expect(g.tagName.toLowerCase()).toBe('svg')
    // Each chart is a polyline over the 30 buckets (area fill + line).
    expect(q.querySelectorAll('polyline')).toHaveLength(1)
    expect(q.querySelectorAll('polygon')).toHaveLength(1)
    // The x-axis shows the first and last UTC day (MM-DD).
    expect(q).toHaveTextContent('09-29')
    expect(q).toHaveTextContent('10-28')
  })

  it('renders one small chart per by_indexer entry, labeled by indexer name', async () => {
    renderStats()
    await screen.findByTestId('indexers-stats-by-indexer')
    const nyaa = screen.getByTestId('indexers-stats-indexer-nyaa')
    const btsou = screen.getByTestId('indexers-stats-indexer-btsou')
    expect(nyaa).toHaveTextContent('nyaa')
    expect(btsou).toHaveTextContent('btsou')
    // Each small chart carries both lines (queries + grabs) and a legend.
    expect(nyaa.querySelectorAll('polyline')).toHaveLength(2)
    expect(nyaa).toHaveTextContent('Queries')
    expect(nyaa).toHaveTextContent('Grabs')
    expect(
      screen.getAllByTestId(/^indexers-stats-indexer-/),
    ).toHaveLength(2)
  })

  it('tolerates an empty by_indexer (no small charts, empty state instead)', async () => {
    ;(client.indexerStatsSummary as ReturnType<typeof vi.fn>).mockResolvedValue(
      seedSummary({
        series: {
          queries: bucketSeries(DAILY),
          grabs: bucketSeries(DAILY),
          by_indexer: [],
        },
      }),
    )
    renderStats()
    await screen.findByTestId('indexers-stats-no-indexers')
    expect(screen.getByTestId('indexers-stats-no-indexers')).toHaveTextContent('No indexer activity')
    expect(screen.queryAllByTestId(/^indexers-stats-indexer-/)).toHaveLength(0)
  })

  it('renders the error state when the summary load fails, with a retry', async () => {
    const user = userEvent.setup()
    ;(client.indexerStatsSummary as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new Error('stats backend down'))
      .mockResolvedValue(seedSummary())
    renderStats()
    await screen.findByTestId('indexers-stats-error')
    expect(screen.getByTestId('indexers-stats-error-message')).toHaveTextContent('stats backend down')
    // Retry re-loads and renders the data.
    await user.click(screen.getByText('Retry'))
    await waitFor(() => expect(screen.getByTestId('indexers-stats-kpis')).toBeInTheDocument())
  })

  it('Refresh re-loads the summary', async () => {
    const user = userEvent.setup()
    renderStats()
    await screen.findByTestId('indexers-stats-refresh')
    await user.click(screen.getByTestId('indexers-stats-refresh'))
    await waitFor(() => expect(client.indexerStatsSummary).toHaveBeenCalledTimes(2))
  })
})
