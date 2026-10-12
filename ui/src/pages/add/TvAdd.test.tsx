import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { TvAddPage } from './TvAdd'

// E2E-style page test for the TV add flow (#42): the acceptance-critical part
// is the TVDB/TMDB source switch — a hit picked from TVDB must carry
// tvdb_id, one from TMDB must carry tmdb_id.
vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  },
  client: {
    searchTv: vi.fn(),
    searchTvTmdb: vi.fn(),
    getProfiles: vi.fn(),
    addSeries: vi.fn(),
  },
}))

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/tv/add']}>
      <Routes>
        <Route path="/tv/add" element={<TvAddPage />} />
        <Route path="/tv" element={<div data-testid="tv-landing">TV library</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const tvdbHit = { id: '82350', title: 'Breaking Bad', year: 2008, extra: { rating: '9.5' } }
const tmdbHit = { id: '1396', title: 'Breaking Bad', year: 2008, extra: { overview: 'Chem teacher.' } }

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.searchTv).mockResolvedValue([tvdbHit])
  vi.mocked(client.searchTvTmdb).mockResolvedValue([tmdbHit])
  vi.mocked(client.getProfiles).mockResolvedValue({ profiles: ['HD-1080p'], default: 'HD-1080p' })
  vi.mocked(client.addSeries).mockResolvedValue({ id: 21 })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('TvAddPage', () => {
  it('sends tvdb_id when the hit came from a TVDB search', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('result-82350'))

    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith({
        title: 'Breaking Bad',
        year: '2008',
        quality_profile: 'HD-1080p',
        monitored: true,
        tvdb_id: '82350',
      }),
    )
    expect(await screen.findByTestId('tv-landing')).toBeInTheDocument()
  })

  it('sends tmdb_id after switching the source to TMDB', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(screen.getByTestId('tv-source-tmdb'))
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('result-1396'))

    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith(
        expect.objectContaining({ tmdb_id: '1396' }),
      ),
    )
  })

  it('a selection made under one source is discarded when the source changes', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('result-82350'))
    expect(screen.getByTestId('tv-selected')).toHaveTextContent('via TVDB')

    await user.click(screen.getByTestId('tv-source-tmdb'))
    // The stale TVDB selection is cleared so its id can't be sent to the
    // wrong provider.
    expect(screen.queryByTestId('tv-selected')).not.toBeInTheDocument()

    await user.type(screen.getByTestId('tv-search-input'), 'bad')
    await user.click(await screen.findByTestId('result-1396'))
    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith(
        expect.objectContaining({ tmdb_id: '1396' }),
      ),
    )
  })

  it('can add manually without a search hit (no provider id)', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-title-input'), 'Some Show')
    await user.type(screen.getByTestId('tv-year-input'), '2019')
    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith({
        title: 'Some Show',
        year: '2019',
        quality_profile: 'HD-1080p',
        monitored: true,
      }),
    )
  })

  it('surfaces the API error inline on a failed add', async () => {
    vi.mocked(client.addSeries).mockRejectedValue(new Error('tvdb_id required for this show'))
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-title-input'), 'No Id Show')
    await user.click(screen.getByTestId('tv-add-button'))
    expect(await screen.findByTestId('tv-add-error')).toHaveTextContent('tvdb_id required')
  })
})
