import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Episode, Series } from '../../api/types'
import { SeriesDetailPage } from './SeriesDetail'

// Page test for the P2 series detail: badge header (the metadata the API
// carries), TheTVDB credit line, season accordions with per-episode toggles,
// and the toolbar's Search Monitored wired to the P2 search endpoint.
vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  },
  client: {
    getSeries: vi.fn(),
    listEpisodes: vi.fn(),
    setEpisodeMonitored: vi.fn(),
    searchMissingEpisodes: vi.fn(),
  },
}))

function fullSeries(over: Partial<Series> = {}): Series {
  return {
    ID: 1,
    TVDBID: 1000,
    TMDBID: 2000,
    Title: 'Breaking Bad',
    Year: 2008,
    QualityProfile: 'HD-1080p',
    Monitored: true,
    SeriesType: 'standard',
    SeasonFolder: false,
    Tags: ['scifi'],
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function ep(id: number, season: number, number: number, title = 'Pilot'): Episode {
  return { ID: id, SeriesID: 1, Season: season, Episode: number, Title: title, Monitored: true, AddedAt: '' }
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/tv/1']}>
      <Routes>
        <Route path="/tv/:id" element={<SeriesDetailPage />} />
        <Route path="/tv" element={<div data-testid="tv-landing">TV library</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.getSeries).mockResolvedValue(
    fullSeries({ Tags: ['scifi'], SeasonFolder: true }),
  )
  vi.mocked(client.listEpisodes).mockResolvedValue([
    ep(11, 1, 1, 'Pilot'),
    ep(12, 1, 2, 'Cat in the Bag…'),
    ep(21, 2, 1, '…'),
  ])
  vi.mocked(client.searchMissingEpisodes).mockResolvedValue({ searched: 3 })
  vi.mocked(client.setEpisodeMonitored).mockResolvedValue({ id: 11, monitored: false })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('SeriesDetailPage (P2)', () => {
  it('renders the badge header and TheTVDB credit', async () => {
    renderDetail()
    expect(await screen.findByTestId('series-detail-header')).toBeInTheDocument()
    expect(screen.getByTestId('badge-monitored')).toHaveTextContent('Monitored')
    expect(screen.getByTestId('badge-quality')).toHaveTextContent('HD-1080p')
    expect(screen.getByTestId('badge-type')).toHaveTextContent('Standard')
    expect(screen.getByTestId('badge-season-folder')).toBeInTheDocument()
    expect(screen.getByTestId('badge-tag-scifi')).toHaveTextContent('scifi')
    expect(screen.getByTestId('series-detail-meta')).toHaveTextContent('TheTVDB')
  })

  it('collapses seasons by default and expands them on click', async () => {
    const user = userEvent.setup()
    renderDetail()
    const season = await screen.findByTestId('season-1')
    // Collapsed: the S1E1 row is not rendered yet.
    expect(screen.queryByTestId('episode-11')).not.toBeInTheDocument()
    const header = within(season).getByTestId('season-toggle-1')
    expect(header).toHaveTextContent('Season 1')
    expect(header).toHaveTextContent('2 episodes')

    await user.click(screen.getByTestId('season-toggle-1'))
    const open = await screen.findByTestId('episode-11')
    expect(open).toBeInTheDocument()
    expect(screen.getByText('Pilot')).toBeInTheDocument()

    // Collapse again.
    await user.click(screen.getByTestId('season-toggle-1'))
    await screen.findByTestId('season-1')
    expect(screen.queryByTestId('episode-11')).not.toBeInTheDocument()
  })

  it('toggles per-episode monitoring', async () => {
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('season-1')
    await user.click(screen.getByTestId('season-toggle-1'))
    await user.click(await screen.findByTestId('episode-mon-11'))
    expect(client.setEpisodeMonitored).toHaveBeenCalledWith(1, 1, 1, false)
  })

  it('Search Monitored calls the P2 endpoint and reports the count', async () => {
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('series-detail-header')
    await user.click(screen.getByTestId('detail-search'))
    expect(await screen.findByTestId('series-detail-status')).toHaveTextContent(
      'Started search for 3 missing episodes',
    )
    expect(client.searchMissingEpisodes).toHaveBeenCalledWith(1)
  })

  it('shows an empty state when the series has no episodes', async () => {
    vi.mocked(client.listEpisodes).mockResolvedValue([])
    renderDetail()
    expect(await screen.findByTestId('series-detail-no-episodes')).toHaveTextContent('No episodes yet')
  })
})
