import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Episode, PageSeries, Series } from '../../api/types'
import { TvPage } from './Tv'

// Page test for the TV list (#41): server-side filtered/paginated series with
// lazy, expand/collapse episode children.
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

function series(id: number, title: string, monitored = true): Series {
  return { ID: id, TVDBID: 1000 + id, TMDBID: 2000 + id, Title: title, Year: 2021, QualityProfile: 'HD', Monitored: monitored, AddedAt: '' }
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

describe('TvPage', () => {
  it('lists series and lazily fetches episodes only on expand', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Breaking Bad'), series(2, 'The Wire')]))
    vi.mocked(client.listEpisodes).mockResolvedValue([ep(11, 1, 1, 1)])
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )

    await screen.findByTestId('series-1')
    expect(screen.getByTestId('series-2')).toHaveTextContent('The Wire')

    // No episode fetch until a row is expanded.
    expect(client.listEpisodes).not.toHaveBeenCalled()

    // Expand the first series: episode list appears, fetched for that series.
    await user.click(screen.getByTestId('series-toggle-1'))
    expect(await screen.findByTestId('episodes-1')).toBeInTheDocument()
    expect(client.listEpisodes).toHaveBeenCalledWith(1)
    expect(screen.getByText('Ep')).toBeInTheDocument()

    // Collapse it again: the expanded block is removed.
    await user.click(screen.getByTestId('series-toggle-1'))
    await waitFor(() => expect(screen.queryByTestId('episodes-1')).not.toBeInTheDocument())
  })

  it('toggles series monitoring and refreshes the list', async () => {
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(1, 'Severance')]))
    vi.mocked(client.setSeriesMonitored).mockResolvedValue({ id: 1, monitored: false })
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <TvPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('series-mon-1')

    await user.click(screen.getByTestId('series-mon-1'))
    expect(client.setSeriesMonitored).toHaveBeenCalledWith(1, false)
    await waitFor(() => expect(client.listSeries).toHaveBeenCalledTimes(2))
  })
})
