import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Episode, PageSeries, Series } from '../../api/types'
import { TvPage } from './Tv'

// Seerr-style TV library (#150): poster grid by default (real PosterURL art
// with an initials stand-in when keyless, kind pill, the legacy unmonitored
// badge, hover refresh/edit), heading control cluster (explicit sort
// direction toggle + sort field dropdown + filter button with a live active
// count), a slide-over drawer (monitored; the genre facets wait on #157),
// and the Sonarr toolbar carried over. The expandable table (P1, with
// per-episode rows) stays fully functional behind the persisted view
// toggle. All data comes from the (faked) API client.
vi.mock('../../api/client', () => ({
  client: {
    listSeries: vi.fn(),
    listEpisodes: vi.fn(),
    getSeries: vi.fn(),
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

function renderWithProbe() {
  function Probe() {
    const location = useLocation()
    return <div data-testid="probe-location">{location.pathname}</div>
  }
  return render(
    <MemoryRouter initialEntries={['/tv']}>
      <Routes>
        <Route
          path="/tv"
          element={
            <>
              <TvPage />
              <Probe />
            </>
          }
        />
        <Route path="/tv/:id" element={<Probe />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  // The per-kind view toggle persists to localStorage; a leak from the
  // list-view test would put later tests in the table view and hide the grid.
  localStorage.clear()
  vi.mocked(client.listSeries).mockReset()
  vi.mocked(client.listEpisodes).mockReset()
  vi.mocked(client.getSeries).mockReset()
  vi.mocked(client.setSeriesMonitored).mockReset()
  vi.mocked(client.setEpisodeMonitored).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('TvPage (#150 grid)', () => {
  it('renders a poster grid by default with quality meta and the kind pill', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire', false)]),
    )
    renderWithProbe()

    const grid = await screen.findByTestId('tv-grid')
    // The quality profile (with the year) is on the card, and the kind pill
    // marks the kind.
    expect(within(grid).getByTestId('series-card-1')).toHaveTextContent('2021 · HD-1080p')
    expect(within(grid).getByTestId('series-card-1')).toHaveTextContent('TV')
    // Unmonitored series keep the legacy "Unmonitored" badge.
    expect(within(grid).getByTestId('series-card-2')).toHaveTextContent('Unmonitored')
    // Monitored series have no status badge (no status field on the wire).
    expect(within(grid).getByTestId('series-card-1').querySelector('.badge')).toBeNull()
  })

  it('sorts server-side: the direction toggle flips order without changing the field', async () => {
    // The (mocked) server returns items in its own (natural) order — the UI
    // must NOT re-sort the fetched page (a leftover client-side sort would
    // have flipped these).
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad', true, 2008), series(2, 'The Wire', true, 1999)]),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('tv-grid')

    // Default sort: title ascending.
    expect(client.listSeries).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'title-asc', page: 1 }),
    )

    // Flipping the direction keeps the field (title) and sends title-desc.
    await user.click(screen.getByTestId('tv-sort-dir'))
    await vi.waitFor(() =>
      expect(client.listSeries).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'title-desc', page: 1 }),
      ),
    )

    // Picking a new field keeps the direction (desc) and sends it server-side.
    await user.click(screen.getByTestId('tv-sort-btn'))
    await user.click(screen.getByTestId('tv-sort-opt-year'))
    await vi.waitFor(() =>
      expect(client.listSeries).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'year-desc', page: 1 }),
      ),
    )

    // The grid renders the server's order, untouched by the client.
    const grid = await screen.findByTestId('tv-grid')
    const titles = [...grid.querySelectorAll<HTMLElement>('[data-testid]')]
      .filter((el) => /^series-card-\d+$/.test(el.dataset.testid ?? ''))
      .map((c) => c.getAttribute('title'))
    expect(titles).toEqual(['Breaking Bad', 'The Wire'])
  })

  it('filters via the drawer (server-side monitored query) and shows the active count', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire', false)]),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('tv-grid')

    await user.click(screen.getByTestId('tv-filter-btn'))
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('0 Active Filters')
    await user.selectOptions(screen.getByTestId('tv-facet-monitored'), 'unmonitored')

    await waitFor(() => {
      const calls = vi.mocked(client.listSeries).mock.calls
      expect(calls[calls.length - 1]?.[0]).toMatchObject({ monitored: false, page: 1 })
    })
    expect(screen.getByTestId('tv-filter-btn')).toHaveTextContent('1 Active Filter')

    // Clear resets to 'all' (no monitored key on the wire).
    await user.click(screen.getByTestId('filter-drawer-clear'))
    await waitFor(() => {
      const calls = vi.mocked(client.listSeries).mock.calls
      const last = calls[calls.length - 1]?.[0]
      expect(last).toBeDefined()
      expect(last?.monitored).toBeUndefined()
    })
    expect(screen.getByTestId('tv-filter-btn')).toHaveTextContent('0 Active Filters')
  })

  it('shows a status line when Update All runs', async () => {
    vi.mocked(client.listSeries)
      .mockResolvedValueOnce(seriesPage([series(1, 'Breaking Bad')]))
      .mockResolvedValueOnce(seriesPage([series(1, 'Breaking Bad')]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('tv-grid')
    await user.click(screen.getByTestId('tv-update-all'))
    expect(await screen.findByTestId('tv-status')).toHaveTextContent('Updated 1 series')
  })

  it('disables the no-backend toolbar buttons with later-phase titles', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Breaking Bad')]))
    renderWithProbe()
    await screen.findByTestId('tv-grid')

    for (const tid of ['tv-rss-sync', 'tv-select-series', 'tv-test-parsing', 'tv-options']) {
      const btn = screen.getByTestId(tid)
      expect(btn).toBeDisabled()
      expect(btn).toHaveAttribute('title', expect.stringMatching(/later phase/))
    }
  })

  it('refreshes a single series through the card hover control', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Severance', true, 2022)]))
    vi.mocked(client.getSeries).mockResolvedValue(series(1, 'Severance', true, 2022))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('card-refresh-1')

    await user.click(screen.getByTestId('card-refresh-1'))
    expect(client.getSeries).toHaveBeenCalledWith(1)
  })

  it('navigates to the series detail page when a card is clicked', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(3, 'Dune')]))
    const user = userEvent.setup()
    renderWithProbe()
    await user.click(await screen.findByTestId('series-card-3-open'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/tv/3')
  })

  it('navigates to the detail page from the card edit control', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(2, 'The Wire')]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('card-edit-2')
    await user.click(screen.getByTestId('card-edit-2'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/tv/2')
  })

  it('toggles between grid and list views', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(
      seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire')]),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('tv-grid')

    await user.click(screen.getByTestId('tv-view-list'))
    expect(screen.queryByTestId('tv-grid')).not.toBeInTheDocument()
    const list = await screen.findByTestId('tv-list')
    expect(within(list).getByTestId('series-1')).toBeInTheDocument()

    await user.click(screen.getByTestId('tv-view-grid'))
    await screen.findByTestId('tv-grid')
  })

  it('list view still lazily fetches episodes on expand and toggles monitoring', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Severance')]))
    vi.mocked(client.listEpisodes).mockResolvedValue([ep(11, 1, 1, 1)])
    vi.mocked(client.setSeriesMonitored).mockResolvedValue({ id: 1, monitored: false })
    const user = userEvent.setup()
    renderWithProbe()
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

  it('shows the empty state with an add shortcut when there are no series', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([]))
    renderWithProbe()
    expect(await screen.findByText('No results')).toBeInTheDocument()
    expect(screen.getByText('Add your first series to get started.')).toBeInTheDocument()
    expect(screen.getByTestId('tv-empty-add')).toHaveAttribute('href', '/tv/add')
  })

  it('surfaces load errors with a retry', async () => {
    vi.mocked(client.listSeries).mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderWithProbe()
    expect(await screen.findByTestId('tv-error')).toBeInTheDocument()
    expect(screen.getByTestId('tv-error-message')).toHaveTextContent('boom')

    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(3, 'Blade Runner')]))
    await user.click(screen.getByText('Retry'))
    expect(await screen.findByTestId('series-card-3')).toBeInTheDocument()
  })
})
