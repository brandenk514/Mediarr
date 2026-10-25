import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Episode, PageSeries, Series } from '../../api/types'
import { TvPage } from './Tv'

// Page test for the P2 Sonarr-parity TV library: 6-column poster grid by
// default (A–Z jump, footer lines, view toggle), with the P1 expandable table
// kept behind the list view.
vi.mock('../../api/client', () => ({
  client: {
    listSeries: vi.fn(),
    listEpisodes: vi.fn(),
    setSeriesMonitored: vi.fn(),
    setEpisodeMonitored: vi.fn(),
  },
}))

function seriesPage(items: Series[], total = items.length, pg = 1): PageSeries {
  return { items, total, page: pg, page_size: 25 }
}

function series(id: number, title: string, monitored = true, year = 2021): Series {
  return {
    ID: id,
    TVDBID: 1000 + id,
    TMDBID: 2000 + id,
    Title: title,
    Year: year,
    QualityProfile: 'HD-1080p',
    Monitored: monitored,
    SeriesType: 'standard',
    SeasonFolder: false,
    Tags: [],
    AddedAt: '2026-01-01T00:00:00Z',
  }
}

function ep(id: number, s: number, season: number, number: number): Episode {
  return { ID: id, SeriesID: s, Season: season, Episode: number, Title: 'Ep', Monitored: true, AddedAt: '' }
}

beforeEach(() => {
  vi.mocked(client.listSeries).mockReset()
  vi.mocked(client.listEpisodes).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('TvPage (P2 grid)', () => {
  it('renders a poster grid by default with monitored + quality footers and a ribbon', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire', false)]),
    )
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )

    const grid = await screen.findByTestId('tv-grid')
    const card1 = within(grid).getByTestId('series-card-1')
    expect(card1).toHaveTextContent('Monitored')
    expect(card1).toHaveTextContent('HD-1080p')

    // Unmonitored series get the red ribbon + footer label.
    const card2 = within(grid).getByTestId('series-card-2')
    expect(card2).toHaveTextContent('Unmonitored')
    expect(within(card2).getByTestId('ribbon-2')).toBeInTheDocument()
  })

  it('shows an A–Z index with only letters that have series', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire')]),
    )
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')
    const az = screen.getByTestId('tv-az')
    expect(az).toHaveTextContent('B')
    // "The Wire" starts with W (leading article skipped).
    expect(within(az).getByLabelText('Jump to W')).toBeInTheDocument()
    // A letter with no series is disabled.
    expect(within(az).getByText('C', { exact: true })).toBeDisabled()
  })

  it('toggles between grid and list views', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire')]),
    )
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')

    await user.click(screen.getByTestId('tv-view-list'))
    expect(screen.queryByTestId('tv-grid')).not.toBeInTheDocument()
    const list = await screen.findByTestId('tv-list')
    expect(within(list).getByTestId('series-1')).toBeInTheDocument()

    await user.click(screen.getByTestId('tv-view-grid'))
    await screen.findByTestId('tv-grid')
  })

  it('sorts the loaded page client-side via the Sort menu', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad', true, 2008), series(2, 'The Wire', true, 1999)]),
    )
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')

    // Default title-asc: Breaking Bad first.
    const cards = () => screen.getAllByTestId(/^series-card-\d+$/).map((c) => c.getAttribute('title'))
    expect(cards()).toEqual(['Breaking Bad (2008)', 'The Wire (1999)'])

    await user.click(screen.getByTestId('tv-sort-btn'))
    await user.click(await screen.findByTestId('tv-sort-opt-year-asc'))
    // 1999 now first.
    expect(cards()).toEqual(['The Wire (1999)', 'Breaking Bad (2008)'])
  })

  it('filters via the Filter menu (server-side monitored query)', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Breaking Bad')]))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')
    await user.click(screen.getByTestId('tv-filter-btn'))
    await user.click(await screen.findByTestId('tv-filter-opt-unmonitored'))
    await waitFor(() => {
      const calls = vi.mocked(client.listSeries).mock.calls
      expect(calls[calls.length - 1]?.[0]).toMatchObject({ monitored: false })
    })
  })

  it('shows a status line when Update All runs', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Breaking Bad')]))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')
    await user.click(screen.getByTestId('tv-update-all'))
    expect(await screen.findByTestId('tv-status')).toHaveTextContent('Updated 1 series')
  })

  it('list view still lazily fetches episodes on expand and toggles monitoring', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Severance')]))
    vi.mocked(client.listEpisodes).mockResolvedValue([ep(11, 1, 1, 1)])
    vi.mocked(client.setSeriesMonitored).mockResolvedValue({ id: 1, monitored: false })
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('tv-grid')
    await user.click(screen.getByTestId('tv-view-list'))
    const list = await screen.findByTestId('tv-list')

    expect(client.listEpisodes).not.toHaveBeenCalled()
    await user.click(within(list).getByTestId('series-toggle-1'))
    expect(await screen.findByTestId('episodes-1')).toBeInTheDocument()
    expect(client.listEpisodes).toHaveBeenCalledWith(1)

    await user.click(within(list).getByTestId('series-mon-1'))
    expect(client.setSeriesMonitored).toHaveBeenCalledWith(1, false)
  })
})
