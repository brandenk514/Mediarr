import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { TvAddPage } from './TvAdd'

// Page test for the P2 two-pane TV add flow: debounced search results on the
// left (selectable rows with badges/overview/provider credit), the add form
// on the right (root folder, monitor, profile, series type, season folder,
// tags, post-add search options, green Add button). The acceptance-critical
// part is the source switch: a TVDB hit carries tvdb_id, a TMDB hit tmdb_id.
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
    searchMissingEpisodes: vi.fn(),
    listRootFolders: vi.fn(),
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

const tvdbHit = { id: '82350', title: 'Breaking Bad', year: 2008, extra: { rating: '9.5', overview: 'Chem teacher.' } }
const tmdbHit = { id: '1396', title: 'Breaking Bad', year: 2008, extra: { overview: 'Chem teacher.' } }

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.searchTv).mockResolvedValue([tvdbHit])
  vi.mocked(client.searchTvTmdb).mockResolvedValue([tmdbHit])
  vi.mocked(client.getProfiles).mockResolvedValue({ profiles: ['HD-1080p'], default: 'HD-1080p' })
  vi.mocked(client.addSeries).mockResolvedValue({ id: 21 })
  vi.mocked(client.searchMissingEpisodes).mockResolvedValue({ searched: 0 })
  vi.mocked(client.listRootFolders).mockResolvedValue([
    { ID: 7, Path: '/media/tv', Name: 'TV', CreatedAt: '', FreeSpace: null },
  ])
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('TvAddPage (P2 two-pane)', () => {
  it('renders selectable result rows with badges, overview and provider credit', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    const row = await screen.findByTestId('tv-result-82350')
    expect(row).toHaveTextContent('Breaking Bad (2008)')
    expect(row).toHaveTextContent('★ 9.5')
    expect(row).toHaveTextContent('Chem teacher.')
    expect(row).toHaveTextContent('via TVDB')

    // The root folder dropdown is populated from the registry.
    const sel = screen.getByTestId('tv-root-folder') as HTMLSelectElement
    expect(sel).toHaveValue('')
    expect(screen.getByTestId('tv-root-folder-help')).toHaveTextContent(
      'subfolder will be created automatically',
    )
  })

  it('sends the full P2 request (tvdb_id, type, season folder, tags) and starts the post-add search', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('tv-result-82350'))

    expect(screen.getByTestId('tv-root-folder-help')).toHaveTextContent("'Breaking Bad' subfolder")
    await user.selectOptions(screen.getByTestId('tv-root-folder'), '7')
    await user.selectOptions(screen.getByTestId('tv-monitor'), 'none')
    await user.selectOptions(screen.getByTestId('tv-series-type'), 'daily')
    await user.click(screen.getByTestId('tv-season-folder'))
    await user.type(screen.getByTestId('tv-tags-input'), 'scifi{Enter}')
    await user.click(screen.getByTestId('tv-start-cutoff'))

    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith({
        title: 'Breaking Bad',
        year: '2008',
        quality_profile: 'HD-1080p',
        monitored: false,
        tvdb_id: '82350',
        series_type: 'daily',
        season_folder: true,
        tags: ['scifi'],
      }),
    )
    expect(client.searchMissingEpisodes).toHaveBeenCalledWith(21)
    expect(await screen.findByTestId('tv-landing')).toBeInTheDocument()
  })

  it('sends tmdb_id after switching the source to TMDB', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(screen.getByTestId('tv-source-tmdb'))
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('tv-result-1396'))

    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith(
        expect.objectContaining({ tmdb_id: '1396', monitored: true }),
      ),
    )
  })

  it('a selection made under one source is discarded when the source changes', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('tv-result-82350'))
    // The picked row is highlighted.
    expect(screen.getByTestId('tv-result-82350')).toHaveClass('sel')

    await user.click(screen.getByTestId('tv-source-tmdb'))
    expect(screen.getByTestId('tv-result-82350')).not.toHaveClass('sel')

    await user.type(screen.getByTestId('tv-search-input'), 'bad')
    await user.click(await screen.findByTestId('tv-result-1396'))
    await user.click(screen.getByTestId('tv-add-button'))
    await waitFor(() =>
      expect(client.addSeries).toHaveBeenCalledWith(
        expect.objectContaining({ tmdb_id: '1396' }),
      ),
    )
  })

  it('disables the add button until a result is selected, then labels it Add <Title>', async () => {
    const user = userEvent.setup()
    renderPage()
    expect(screen.getByTestId('tv-add-button')).toBeDisabled()
    expect(screen.getByTestId('tv-add-button')).toHaveTextContent('Add series')

    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('tv-result-82350'))
    const btn = screen.getByTestId('tv-add-button')
    expect(btn).toBeEnabled()
    expect(btn).toHaveTextContent('Add Breaking Bad')
  })

  it('surfaces the API error inline on a failed add', async () => {
    vi.mocked(client.addSeries).mockRejectedValue(new Error('tvdb_id required for this show'))
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('tv-search-input'), 'breaking')
    await user.click(await screen.findByTestId('tv-result-82350'))
    await user.click(screen.getByTestId('tv-add-button'))
    expect(await screen.findByTestId('tv-add-error')).toHaveTextContent('tvdb_id required')
  })
})
