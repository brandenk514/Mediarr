import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Artist, PageArtist } from '../../api/types'
import { MusicPage } from './Music'

// Page test for the Lidarr L1 artist grid: square-cover cards with the
// five-state status ribbon + red corner fold on flagged (unmonitored)
// artists, the library footer status bar (GET /music/library/summary),
// the ribbon legend, the A–Z quick jump, the Lidarr-ordered toolbar, and
// the grid/list view toggle.
vi.mock('../../api/client', () => ({
  client: {
    listArtists: vi.fn(),
    setArtistMonitored: vi.fn(),
    getMusicLibrarySummary: vi.fn(),
  },
}))

function page(items: Artist[], total = items.length): PageArtist {
  return { items, total, page: 1, page_size: 25 }
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

beforeEach(() => {
  vi.mocked(client.listArtists).mockReset()
  vi.mocked(client.setArtistMonitored).mockReset()
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

describe('MusicPage (L1 artist grid)', () => {
  it('renders square-cover cards with the three-line footer, ribbon and corner fold', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(
      page([artist(1, { Name: 'Radiohead', QualityProfile: 'FLAC', Monitored: false })]),
    )
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )

    await screen.findByTestId('artist-card-1')
    // Footer: bold name, Monitored line, quality profile line.
    const foot = screen.getByTestId('artist-card-1').querySelector('.music-foot')!
    expect(foot.querySelector('.mf-name')?.textContent).toBe('Radiohead')
    expect(foot.querySelector('.mf-mon')?.textContent).toBe('Unmonitored')
    expect(screen.getByTestId('card-quality-1')).toHaveTextContent('FLAC')
    // Unmonitored = flagged: the red corner fold is present.
    expect(screen.getByTestId('artist-flag-1')).toBeInTheDocument()
  })

  it('falls back to the kind default profile label when no quality profile is set', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1, { Name: 'No Profile' })]))
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    expect(screen.getByTestId('card-quality-1')).toHaveTextContent('Lossless')
  })

  it('derives all five ribbon states client-side from the per-artist counts', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page(ribbonStateArtists()))
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )

    await screen.findByTestId('artist-card-1')
    expect(screen.getByTestId('ribbon-1').getAttribute('data-ribbon-state')).toBe('downloading')
    expect(screen.getByTestId('ribbon-2').getAttribute('data-ribbon-state')).toBe('continuing')
    expect(screen.getByTestId('ribbon-3').getAttribute('data-ribbon-state')).toBe('ended')
    expect(screen.getByTestId('ribbon-4').getAttribute('data-ribbon-state')).toBe('missing-monitored')
    expect(screen.getByTestId('ribbon-5').getAttribute('data-ribbon-state')).toBe('missing-unmonitored')
  })

  it('shows the ribbon legend with the five states', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page([artist(1)]))
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
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
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    const summary = await screen.findByTestId('music-summary')
    expect(screen.getByTestId('summary-artists')).toHaveTextContent('5')
    expect(screen.getByTestId('summary-inactive')).toHaveTextContent('1')
    expect(screen.getByTestId('summary-continuing')).toHaveTextContent('2')
    expect(screen.getByTestId('summary-monitored')).toHaveTextContent('4')
    expect(screen.getByTestId('summary-unmonitored')).toHaveTextContent('1')
    expect(screen.getByTestId('summary-tracks')).toHaveTextContent('50')
    expect(screen.getByTestId('summary-files')).toHaveTextContent('25')
    expect(screen.getByTestId('summary-size')).toHaveTextContent('1 GiB')
    expect(summary).toBeInTheDocument()
  })

  it('renders the A–Z quick jump with only letters that have entries', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(
      page([artist(1, { Name: 'Radiohead' }), artist(2, { Name: 'AC/DC' })]),
    )
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    const az = await screen.findByTestId('music-az')
    expect(az).toBeInTheDocument()
    // Only A and R are enabled.
    const enabled = Array.from(az.querySelectorAll('button.has'))
    expect(enabled.map((b) => b.textContent)).toEqual(['A', 'R'])
  })

  it('shows the Lidarr-ordered toolbar groups and updates all', async () => {
    vi.mocked(client.listArtists)
      .mockResolvedValueOnce(page([artist(1), artist(2)]))
      .mockResolvedValueOnce(page([artist(1), artist(2)]))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')

    // Left group: Update All, RSS Sync | Select Artists; right group:
    // Options | View · Sort · Filter.
    expect(screen.getByTestId('music-update-all')).toBeInTheDocument()
    expect(screen.getByTestId('music-rss-sync')).toBeDisabled()
    expect(screen.getByTestId('music-select')).toBeDisabled()
    expect(screen.getByTestId('music-options')).toBeDisabled()
    expect(screen.getByTestId('music-view-grid')).toBeInTheDocument()
    expect(screen.getByTestId('music-sort-btn')).toBeInTheDocument()
    expect(screen.getByTestId('music-filter-btn')).toBeInTheDocument()

    await user.click(screen.getByTestId('music-update-all'))
    expect(await screen.findByTestId('music-status')).toHaveTextContent('Updated 2 artists just now')
    expect(client.listArtists).toHaveBeenCalledTimes(2)
  })

  it('filters the grid by ribbon state through the Filter menu', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(page(ribbonStateArtists()))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    expect(screen.getByTestId('music-grid').children.length).toBe(5)

    await user.click(screen.getByTestId('music-filter-btn'))
    // Live counts appear in the status-group options.
    await user.click(await screen.findByTestId('music-filter-opt-missing-monitored'))
    expect(screen.getByTestId('music-grid').children.length).toBe(1)
    expect(screen.getByTestId('artist-card-4')).toBeInTheDocument()
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
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    await user.click(screen.getByTestId('music-view-list'))

    const table = await screen.findByTestId('music-table')
    expect(table).toBeInTheDocument()
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
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-card-1')
    await user.click(screen.getByTestId('music-view-list'))
    const toggle = await screen.findByTestId('artist-mon-1')
    await user.click(toggle)
    expect(client.setArtistMonitored).toHaveBeenCalledWith(1, false)
  })
})
