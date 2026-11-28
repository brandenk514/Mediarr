import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Artist, PageArtist } from '../../api/types'
import { MusicPage } from './Music'

// Seerr-style artist library (#150): poster grid by default (cover art with
// an initials stand-in when keyless, kind pill, the five-state Lidarr ribbon
// as a status badge, hover refresh/edit), heading control cluster (explicit
// sort direction toggle + sort field dropdown + filter button with a live
// active count), a slide-over filter drawer (monitored / genre / status)
// that filters server-side, and the Lidarr toolbar + library summary bar
// carried over. The table view stays fully functional behind the persisted
// view toggle. All data comes from the (faked) API client.
vi.mock('../../api/client', () => ({
  client: {
    listArtists: vi.fn(),
    setArtistMonitored: vi.fn(),
    getArtist: vi.fn(),
    getMusicLibrarySummary: vi.fn(),
  },
}))

function page(items: Artist[], total = items.length, pg = 1): PageArtist {
  return { items, total, page: pg, page_size: 25 }
}

function artist(id: number, over: Partial<Artist> = {}): Artist {
  return {
    ID: id,
    MBID: `mbid-${id}`,
    Name: `Artist ${id}`,
    Monitored: true,
    Status: 'continuing',
    TrackCount: 10,
    FileCount: 10,
    QueuedCount: 0,
    TotalSize: 0,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

// One artist per ribbon state (the five Lidarr grid states).
function ribbonStateArtists(): Artist[] {
  return [
    artist(1, { Name: 'Downloading Artist', QueuedCount: 2 }),
    artist(2, { Name: 'Continuing Artist', Status: 'continuing', FileCount: 10, TrackCount: 10 }),
    artist(3, { Name: 'Ended Artist', Status: 'ended', FileCount: 10, TrackCount: 10 }),
    artist(4, { Name: 'Missing Monitored', FileCount: 3, TrackCount: 10, Monitored: true }),
    artist(5, { Name: 'Missing Unmonitored', FileCount: 3, TrackCount: 10, Monitored: false }),
  ]
}

function renderWithProbe() {
  function Probe() {
    const location = useLocation()
    return <div data-testid="probe-location">{location.pathname}</div>
  }
  return render(
    <MemoryRouter initialEntries={['/music']}>
      <Routes>
        <Route
          path="/music"
          element={
            <>
              <MusicPage />
              <Probe />
            </>
          }
        />
        <Route path="/music/:id" element={<Probe />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  // The per-kind view toggle persists to localStorage; a leak from the
  // list-view test would put later tests in the table view and hide the grid.
  localStorage.clear()
  vi.mocked(client.listArtists).mockReset()
  vi.mocked(client.setArtistMonitored).mockReset()
  vi.mocked(client.getArtist).mockReset()
  vi.mocked(client.getMusicLibrarySummary).mockReset()
  vi.mocked(client.getMusicLibrarySummary).mockResolvedValue({
    artists: 5,
    inactive: 1,
    continuing: 2,
    monitored: 4,
    unmonitored: 1,
    tracks: 50,
    files: 25,
    total_file_size: 1073741824, // 1 GiB
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MusicPage (#150 artist grid)', () => {
  it('renders the five ribbon states as status badges on the grid cards', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page(ribbonStateArtists()))
    renderWithProbe()

    expect(await screen.findByTestId('music-grid')).toBeInTheDocument()
    expect(screen.getByTestId('artist-card-1')).toHaveTextContent('Downloading')
    expect(screen.getByTestId('artist-card-2')).toHaveTextContent('Continuing (all tracks downloaded)')
    expect(screen.getByTestId('artist-card-3')).toHaveTextContent('Ended (all tracks downloaded)')
    expect(screen.getByTestId('artist-card-4')).toHaveTextContent('Missing Tracks (artist monitored)')
    expect(screen.getByTestId('artist-card-5')).toHaveTextContent('Missing Tracks (artist not monitored)')
    // The kind pill and the quality profile (with the kind default fallback)
    // are on the card.
    expect(screen.getByTestId('artist-card-1')).toHaveTextContent('Music')
    expect(screen.getByTestId('artist-card-2')).toHaveTextContent('Lossless')
  })

  it('shows the ribbon legend with the five states', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1)]))
    renderWithProbe()
    await screen.findByTestId('artist-card-1')
    const legend = await screen.findByTestId('music-legend')
    expect(legend).toHaveTextContent('Downloading')
    expect(legend).toHaveTextContent('Continuing (all tracks downloaded)')
    expect(legend).toHaveTextContent('Ended (all tracks downloaded)')
    expect(legend).toHaveTextContent('Missing Tracks (artist monitored)')
    expect(legend).toHaveTextContent('Missing Tracks (artist not monitored)')
  })

  it('populates the footer status bar from the library summary endpoint', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1)]))
    renderWithProbe()
    await screen.findByTestId('artist-card-1')
    expect(await screen.findByTestId('music-summary')).toBeInTheDocument()
    expect(screen.getByTestId('summary-artists')).toHaveTextContent('5')
    expect(screen.getByTestId('summary-inactive')).toHaveTextContent('1')
    expect(screen.getByTestId('summary-continuing')).toHaveTextContent('2')
    expect(screen.getByTestId('summary-monitored')).toHaveTextContent('4')
    expect(screen.getByTestId('summary-unmonitored')).toHaveTextContent('1')
    expect(screen.getByTestId('summary-tracks')).toHaveTextContent('50')
    expect(screen.getByTestId('summary-files')).toHaveTextContent('25')
    expect(screen.getByTestId('summary-size')).toHaveTextContent('1 GiB')
  })

  it('renders the Lidarr-ordered toolbar and updates all', async () => {
    vi.mocked(client.listArtists)
      .mockResolvedValueOnce(page([artist(1), artist(2)]))
      .mockResolvedValueOnce(page([artist(1), artist(2)]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('artist-card-1')

    expect(screen.getByTestId('music-update-all')).toBeInTheDocument()
    expect(screen.getByTestId('music-rss-sync')).toBeDisabled()
    expect(screen.getByTestId('music-select')).toBeDisabled()
    expect(screen.getByTestId('music-options')).toBeDisabled()
    expect(screen.getByTestId('music-view-grid')).toBeInTheDocument()
    expect(screen.getByTestId('music-filter-btn')).toBeInTheDocument()

    await user.click(screen.getByTestId('music-update-all'))
    expect(await screen.findByTestId('music-status')).toHaveTextContent('Updated 2 artists just now')
    expect(client.listArtists).toHaveBeenCalledTimes(2)
  })

  it('sorts server-side: the direction toggle flips order without changing the field', async () => {
    // The (mocked) server returns items in its own order — the UI must NOT
    // re-sort the fetched page (a leftover client-side sort would have
    // flipped these).
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1, { Name: 'Alpha' }), artist(2, { Name: 'Zulu' })]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('music-grid')

    // Default sort: name ascending.
    expect(client.listArtists).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'title-asc', page: 1 }),
    )

    // Flipping the direction keeps the field (title) and sends title-desc.
    await user.click(screen.getByTestId('music-sort-dir'))
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'title-desc', page: 1 }),
      ),
    )

    // Picking a new field keeps the direction (desc) and sends it server-side.
    await user.click(screen.getByTestId('music-sort-btn'))
    await user.click(screen.getByTestId('music-sort-opt-added'))
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'added-desc', page: 1 }),
      ),
    )

    // The grid renders the server's order, untouched by the client.
    const grid = await screen.findByTestId('music-grid')
    const titles = [...grid.querySelectorAll<HTMLElement>('[data-testid]')]
      .filter((el) => /^artist-card-\d+$/.test(el.dataset.testid ?? ''))
      .map((c) => c.getAttribute('title'))
    expect(titles).toEqual(['Alpha', 'Zulu'])
  })

  it('filters server-side through the drawer: monitored, genre and status', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page(ribbonStateArtists()))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('music-grid')

    await user.click(screen.getByTestId('music-filter-btn'))
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('0 Active Filters')

    // Monitored facet -> server monitored=false.
    await user.selectOptions(screen.getByTestId('music-facet-monitored'), 'unmonitored')
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({ monitored: false, page: 1 }),
      ),
    )
    // Genre free text -> server filters.genre.
    await user.type(screen.getByTestId('music-facet-genre'), 'Metal')
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: expect.objectContaining({ genre: 'Metal' }),
        }),
      ),
    )
    // Status select -> server filters.status.
    await user.selectOptions(screen.getByTestId('music-facet-status'), 'ended')
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: expect.objectContaining({ genre: 'Metal', status: 'ended' }),
        }),
      ),
    )
    // Three active filters, pluralized in the heading button.
    expect(screen.getByTestId('music-filter-btn')).toHaveTextContent('3 Active Filters')

    // Clear Active Filters resets every facet and refetches unfiltered
    // ('all' sends no monitored key at all — see usePagedList).
    await user.click(screen.getByTestId('filter-drawer-clear'))
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith(
        expect.objectContaining({
          filters: expect.objectContaining({ genre: '', status: '' }),
        }),
      ),
    )
    const calls = vi.mocked(client.listArtists).mock.calls
    const lastQuery = calls[calls.length - 1]?.[0]
    expect(lastQuery).toBeDefined()
    expect(lastQuery?.monitored).toBeUndefined()
    expect(screen.getByTestId('music-filter-btn')).toHaveTextContent('0 Active Filters')
  })

  it('refreshes a single artist through the card hover control', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(
      page([artist(1, { Name: 'Radiohead', FileCount: 3, TrackCount: 10, Status: 'ended' })]),
    )
    // The fresh aggregate: all tracks downloaded + ended -> the ribbon
    // flips from Missing to Ended.
    vi.mocked(client.getArtist).mockResolvedValue(
      artist(1, { Name: 'Radiohead', FileCount: 10, TrackCount: 10, Status: 'ended' }),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('card-refresh-1')
    expect(screen.getByTestId('artist-card-1')).toHaveTextContent('Missing Tracks')

    await user.click(screen.getByTestId('card-refresh-1'))
    expect(client.getArtist).toHaveBeenCalledWith(1)
    await vi.waitFor(() =>
      expect(screen.getByTestId('artist-card-1')).toHaveTextContent('Ended (all tracks downloaded)'),
    )
  })

  it('navigates to the artist detail page when a card is clicked', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(3, { Name: 'Dune' })]))
    const user = userEvent.setup()
    renderWithProbe()
    await user.click(await screen.findByTestId('artist-card-3-open'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/music/3')
  })

  it('navigates to the detail page from the card edit control', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(5, { Name: 'Tenet' })]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('card-edit-5')
    await user.click(screen.getByTestId('card-edit-5'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/music/5')
  })

  it('toggles between grid and list views; the list view keeps a table', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(
      page([
        artist(1, {
          Name: 'Radiohead',
          QualityProfile: 'FLAC',
          TrackCount: 12,
          FileCount: 5,
          TotalSize: 2048,
          Status: 'continuing',
        }),
      ]),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('artist-card-1')
    await user.click(screen.getByTestId('music-view-list'))

    const table = await screen.findByTestId('music-table')
    expect(table).toHaveTextContent('Radiohead')
    expect(table).toHaveTextContent('12') // tracks
    expect(table).toHaveTextContent('5') // files
    expect(table).toHaveTextContent('2 KiB')
    expect(screen.queryByTestId('music-grid')).not.toBeInTheDocument()
  })

  it('toggles an artist monitored from the list view', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1, { Name: 'Radiohead' })]))
    vi.mocked(client.setArtistMonitored).mockResolvedValue({ id: 1, monitored: false })
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('artist-card-1')
    await user.click(screen.getByTestId('music-view-list'))
    const toggle = await screen.findByTestId('artist-mon-1')
    await user.click(toggle)
    expect(client.setArtistMonitored).toHaveBeenCalledWith(1, false)
  })

  it('shows the empty state with an add shortcut when there are no artists', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([]))
    renderWithProbe()
    expect(await screen.findByText('No results')).toBeInTheDocument()
    expect(screen.getByText('Add your first artist to get started.')).toBeInTheDocument()
    expect(screen.getByTestId('music-empty-add')).toHaveAttribute('href', '/music/add')
  })

  it('surfaces load errors with a retry', async () => {
    vi.mocked(client.listArtists).mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderWithProbe()
    expect(await screen.findByTestId('music-error')).toBeInTheDocument()
    expect(screen.getByTestId('music-error-message')).toHaveTextContent('boom')

    vi.mocked(client.listArtists).mockResolvedValue(page([artist(3, { Name: 'Blade Runner' })]))
    await user.click(screen.getByText('Retry'))
    expect(await screen.findByTestId('artist-card-3')).toBeInTheDocument()
  })
})
