import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { PageWantedItem, WantedItem } from '../../api/types'
import { WantedPage } from './Wanted'

vi.mock('../../api/client', () => ({
  client: {
    listWanted: vi.fn(),
    searchWanted: vi.fn(),
    unmonitorWanted: vi.fn(),
    manualImport: vi.fn(),
  },
}))

function wantedItems(): WantedItem[] {
  return [
    { kind: 'tv', series_id: 10, series_title: 'Severance', season: 1, episode: 1, episode_title: 'Pilot', air_date: '2022-01-11', status: 'missing: no file' },
    { kind: 'tv', series_id: 20, series_title: 'Slow Horses', season: 2, episode: 1, episode_title: 'Episode 1', air_date: null, status: 'missing: no file' },
  ]
}

function pageData(): PageWantedItem {
  return { items: wantedItems(), total: 2, page: 1, pageSize: 10 }
}

function renderPage(tab: 'missing' | 'cutoff' = 'missing') {
  return render(
    <MemoryRouter initialEntries={['/wanted/missing']}>
      <WantedPage tab={tab} />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listWanted).mockReset()
  vi.mocked(client.searchWanted).mockReset()
  vi.mocked(client.unmonitorWanted).mockReset()
  vi.mocked(client.manualImport).mockReset()
})

describe('WantedPage (missing)', () => {
  it('loads missing episodes and renders a row per episode', async () => {
    vi.mocked(client.listWanted).mockResolvedValue(pageData() as never)
    renderPage('missing')
    expect(screen.getByText('Loading wanted…')).toBeInTheDocument()
    await waitFor(() => expect(client.listWanted).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('Severance')).toBeInTheDocument()
    expect(screen.getByText('Slow Horses')).toBeInTheDocument()
    expect(screen.getByTestId('wanted-status-10-1-1')).toHaveTextContent('missing: no file')
  })

  it('searches all missing episodes via the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listWanted).mockResolvedValue(pageData() as never)
    vi.mocked(client.searchWanted).mockResolvedValue({ searched: 2 } as never)
    renderPage('missing')
    await waitFor(() => expect(screen.getByTestId('wanted-search-missing')).toBeInTheDocument())
    await user.click(screen.getByTestId('wanted-search-missing'))
    await waitFor(() => expect(client.searchWanted).toHaveBeenCalledWith('missing'))
    expect(await screen.findByTestId('wanted-msg-missing')).toHaveTextContent('2')
  })

  it('unmonitors the selected episodes via the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listWanted).mockResolvedValue(pageData() as never)
    vi.mocked(client.unmonitorWanted).mockResolvedValue({ unmonitored: 1 } as never)
    renderPage('missing')
    await waitFor(() => expect(screen.getByTestId('wanted-check-10-1-1')).toBeInTheDocument())
    await user.click(screen.getByTestId('wanted-check-10-1-1'))
    await user.click(screen.getByTestId('wanted-unmonitor-missing'))
    await waitFor(() =>
      expect(client.unmonitorWanted).toHaveBeenCalledWith([{ series_id: 10, season: 1, episode: 1 }]),
    )
  })

  it('opens the manual import dialog and imports a single episode', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listWanted).mockResolvedValue(pageData() as never)
    vi.mocked(client.manualImport).mockResolvedValue({ imported: true, message: 'imported OK' } as never)
    renderPage('missing')
    await waitFor(() => expect(screen.getByTestId('wanted-import-10-1-1')).toBeInTheDocument())
    await user.click(screen.getByTestId('wanted-import-10-1-1'))
    expect(await screen.findByTestId('wanted-import-modal')).toBeInTheDocument()
    await user.type(screen.getByTestId('wanted-import-path'), '/media/tv/Severance/S01E01.mkv')
    await user.click(screen.getByTestId('wanted-import-submit'))
    await waitFor(() =>
      expect(client.manualImport).toHaveBeenCalledWith({
        kind: 'tv',
        series_id: 10,
        season: 1,
        episode: 1,
        file_path: '/media/tv/Severance/S01E01.mkv',
      }),
    )
  })

  it('shows an empty state when there are no missing episodes', async () => {
    vi.mocked(client.listWanted).mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 10 } as never)
    renderPage('missing')
    expect(await screen.findByTestId('wanted-empty-missing')).toBeInTheDocument()
  })

  it('shows an error state on load failure', async () => {
    vi.mocked(client.listWanted).mockRejectedValue(new Error('boom'))
    renderPage('missing')
    await waitFor(() => expect(screen.getByTestId('wanted-error-missing')).toBeInTheDocument())
    expect(screen.getByTestId('wanted-error-missing')).toHaveTextContent('boom')
  })
})
