import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Album, Artist, MusicHistoryEntry, PageArtist, Track } from '../../api/types'
import { ArtistDetailPage } from './ArtistDetail'

// Page test for the Lidarr L1 artist detail: banner header (name, rating,
// genre, green pill row, prev/next paging), the toolbar (Lidarr order,
// stubs disabled), the albums accordion grouped by album type (count badge
// + group size), album rows, disc sections, the track table's Audio Info
// rendering, Expand All, the history modal, the monitoring modal, and the
// L1 edit modal.
vi.mock('../../api/client', () => ({
  client: {
    getArtist: vi.fn(),
    listAlbums: vi.fn(),
    listArtists: vi.fn(),
    listTracks: vi.fn(),
    runMusicPipeline: vi.fn(),
    getArtistHistory: vi.fn(),
    setArtistMonitored: vi.fn(),
    setAlbumMonitored: vi.fn(),
    // Lidarr L2: the full Edit Artist modal persists through updateArtist,
    // and its Quality Profile picker reads the music profiles.
    getProfiles: vi.fn(),
    updateArtist: vi.fn(),
  },
}))

function artist(id: number, over: Partial<Artist> = {}): Artist {
  return {
    ID: id,
    MBID: `mbid-${id}`,
    Name: `Artist ${id}`,
    Monitored: true,
    Status: 'continuing',
    QualityProfile: 'FLAC',
    Path: 'Music/Artist',
    Rating: 0,
    Genre: '',
    Links: [],
    TrackCount: 0,
    FileCount: 0,
    QueuedCount: 0,
    TotalSize: 0,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function album(id: number, over: Partial<Album> = {}): Album {
  return {
    ID: id,
    ArtistID: 7,
    MBID: `alb-${id}`,
    Name: `Album ${id}`,
    Year: 2000,
    QualityProfile: 'FLAC',
    Monitored: true,
    VerifyChecksums: false,
    Checksum: '',
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function track(id: number, over: Partial<Track> = {}): Track {
  return {
    ID: id,
    AlbumID: 11,
    MBID: `trk-${id}`,
    Disc: 1,
    Number: id,
    Title: `Track ${id}`,
    Checksum: '',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function libPage(items: Artist[]): PageArtist {
  return { items, total: items.length, page: 1, page_size: 25 }
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/music/7']}>
      <Routes>
        <Route path="/music/:id" element={<ArtistDetailPage />} />
        <Route path="/music" element={<div data-testid="music-landing">Artists</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.getArtist).mockReset()
  vi.mocked(client.listAlbums).mockReset()
  vi.mocked(client.listArtists).mockReset()
  vi.mocked(client.listTracks).mockReset()
  vi.mocked(client.runMusicPipeline).mockReset()
  vi.mocked(client.getArtistHistory).mockReset()
  vi.mocked(client.setArtistMonitored).mockReset()
  vi.mocked(client.setAlbumMonitored).mockReset()
  vi.mocked(client.getProfiles).mockReset()
  vi.mocked(client.updateArtist).mockReset()
  vi.mocked(client.getArtist).mockResolvedValue(
    artist(7, {
      Name: 'Radiohead',
      Rating: 87,
      Genre: 'Alternative',
      Path: 'Music/Radiohead',
      TotalSize: 1073741824, // 1 GiB
      Links: [{ Name: 'Wikipedia', URL: 'https://en.wikipedia.org/wiki/Radiohead' }],
    }),
  )
  vi.mocked(client.listArtists).mockResolvedValue(libPage([artist(1, { Name: 'Alpha' }), artist(7, { Name: 'Radiohead' }), artist(2, { Name: 'Beta' })]))
  vi.mocked(client.listAlbums).mockResolvedValue([])
  vi.mocked(client.listTracks).mockResolvedValue([])
  vi.mocked(client.runMusicPipeline).mockResolvedValue({ imported: 'In Rainbows.flac' })
  vi.mocked(client.getArtistHistory).mockResolvedValue([])
  vi.mocked(client.setArtistMonitored).mockResolvedValue({ id: 7, monitored: false })
  vi.mocked(client.setAlbumMonitored).mockResolvedValue({ id: 11, monitored: false })
  vi.mocked(client.getProfiles).mockResolvedValue({ profiles: ['FLAC', 'MP3 320'], default: 'FLAC' })
  vi.mocked(client.updateArtist).mockResolvedValue({ id: 7, monitored: true })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('ArtistDetailPage', () => {
  it('renders the banner (name, rating, genre, pills, links) and the toolbar', async () => {
    renderDetail()
    await screen.findByTestId('artist-detail')

    expect(screen.getByTestId('artist-detail-name')).toHaveTextContent('Radiohead')
    expect(screen.getByTestId('artist-detail-rating')).toHaveTextContent('♥ 87%')
    expect(screen.getByTestId('artist-detail-genre')).toHaveTextContent('Alternative')
    expect(screen.getByTestId('pill-path')).toHaveTextContent('Music/Radiohead')
    expect(screen.getByTestId('pill-size')).toHaveTextContent('1 GiB')
    expect(screen.getByTestId('pill-profile')).toHaveTextContent('FLAC')
    expect(screen.getByTestId('pill-monitored')).toHaveTextContent('Monitored')
    expect(screen.getByTestId('pill-status')).toHaveTextContent('Continuing')
    expect(screen.getByTestId('pill-link-Wikipedia')).toHaveAttribute('href', 'https://en.wikipedia.org/wiki/Radiohead')

    // Toolbar: Lidarr order, with the L1 stubs disabled.
    for (const id of [
      'artist-detail-refresh',
      'artist-detail-search',
      'artist-detail-interactive',
      'artist-detail-rename',
      'artist-detail-retag',
      'artist-detail-manage-tracks',
      'artist-detail-history',
      'artist-detail-manual-import',
      'artist-detail-monitoring',
      'artist-detail-edit',
      'artist-detail-delete',
      'artist-detail-expand-all',
    ]) {
      expect(screen.getByTestId(id), id).toBeInTheDocument()
    }
    expect(screen.getByTestId('artist-detail-interactive')).toBeDisabled()
    expect(screen.getByTestId('artist-detail-delete')).toBeDisabled()
  })

  it('hides the rating, size and links when unknown', async () => {
    vi.mocked(client.getArtist).mockResolvedValue(
      artist(7, { Name: 'Bare', Rating: 0, TotalSize: 0, Links: [] }),
    )
    renderDetail()
    await screen.findByTestId('artist-detail')
    expect(screen.queryByTestId('artist-detail-rating')).not.toBeInTheDocument()
    expect(screen.queryByTestId('pill-size')).not.toBeInTheDocument()
    expect(screen.queryByTestId('pill-link-Wikipedia')).not.toBeInTheDocument()
  })

  it('shows prev/next paging arrows in library order', async () => {
    renderDetail()
    await screen.findByTestId('artist-detail')
    // Radiohead is #2 of 3 in the library order.
    expect(screen.getByTestId('artist-detail-prev')).toBeInTheDocument()
    expect(screen.getByTestId('artist-detail-next')).toBeInTheDocument()
  })

  it('runs the artist pipeline from Refresh & Scan', async () => {
    renderDetail()
    const user = userEvent.setup()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-refresh'))
    expect(await screen.findByTestId('artist-detail-status')).toHaveTextContent('In Rainbows.flac')
    expect(client.runMusicPipeline).toHaveBeenCalledWith(7)
  })

  it('groups albums by album type with the count badge and group size', async () => {
    vi.mocked(client.listAlbums).mockResolvedValue([
      album(11, { Name: 'In Rainbows', Year: 2007, TrackCount: 9, FileCount: 6, Size: 500 }),
      album(12, { Name: 'The Bends', Year: 1995, TrackCount: 3, FileCount: 0, Size: 0 }),
      album(13, { Name: 'Fake News', AlbumType: 'single', Year: 2008, TrackCount: 2, FileCount: 2, Size: 0 }),
    ])
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')

    // Two groups: Album (2) and Single (1), in Lidarr's group order.
    expect(screen.getByTestId('type-group-album')).toBeInTheDocument()
    expect(screen.getByTestId('type-group-single')).toBeInTheDocument()
    // Count badge = FileCount/TrackCount across the group; size = sum (0 hidden).
    expect(screen.getByTestId('type-count-album')).toHaveTextContent('6/12')
    expect(screen.getByTestId('type-size-album')).toHaveTextContent('500 B')
    expect(screen.getByTestId('type-count-single')).toHaveTextContent('2/2')
    expect(screen.queryByTestId('type-size-single')).not.toBeInTheDocument()

    // Expand the Album group: album rows with title, date, status.
    await user.click(screen.getByTestId('type-toggle-album'))
    expect(await screen.findByTestId('album-11')).toHaveTextContent('In Rainbows')
    expect(screen.getByTestId('album-date-11')).toHaveTextContent('2007')
    expect(screen.getByTestId('album-status-11')).toHaveTextContent('Missing') // 6 of 9 downloaded
    expect(screen.getByTestId('album-12')).toHaveTextContent('The Bends')
    expect(screen.getByTestId('album-status-12')).toHaveTextContent('Missing')
  })

  it('expands an album into the sub-header, disc sections and track table', async () => {
    vi.mocked(client.listAlbums).mockResolvedValue([
      album(11, { Name: 'In Rainbows', TrackCount: 3, FileCount: 2, Duration: 1200, Size: 1024, ReleaseDate: '2007-10-10', Rating: 91 }),
    ])
    vi.mocked(client.listTracks).mockImplementation(async (albumId: number) => {
      if (albumId === 11) {
        return [
          track(1, {
            Number: 1,
            Title: '15 Step',
            Duration: 262,
            Codec: 'FLAC',
            Channels: 2,
            Kbps: 994,
            SampleRate: 44,
            BitDepth: 16,
            Status: 'FLAC',
          }),
          track(2, { Number: 2, Title: 'Bodysnatchers', Duration: 0 }), // no audio metadata
          track(3, { Number: 3, Title: 'Videotape', Disc: 2, Status: '' }), // missing, disc 2
        ]
      }
      return []
    })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')

    // Tracks are lazy: nothing fetched until the album expands.
    expect(client.listTracks).not.toHaveBeenCalled()
    await user.click(screen.getByTestId('type-toggle-album'))
    await user.click(await screen.findByTestId('album-toggle-11'))

    // Sub-header: duration, rating, release date, size, monitored state.
    const sub = await screen.findByTestId('album-sub-11')
    expect(sub).toHaveTextContent('20:00')
    expect(sub).toHaveTextContent('♥ 91%')
    expect(sub).toHaveTextContent('2007-10-10')
    expect(sub).toHaveTextContent('1 KiB')
    expect(screen.getByTestId('album-monitored-11')).toHaveTextContent('Monitored')

    // Two disc sections; 'CD 1 — 1/2' (only track 1 has a file).
    expect(screen.getByTestId('disc-11-1')).toHaveTextContent('CD 1 — 1/2')
    expect(screen.getByTestId('disc-11-2')).toHaveTextContent('CD 2 — 0/1')

    // Track table: Audio Info joins the known parts, omits the unknown,
    // and is an em-dash when the track has no audio metadata at all.
    expect(screen.getByTestId('track-audio-11-1')).toHaveTextContent('FLAC - 2 - 994 kbps - 44 kHz - 16 bit')
    expect(screen.getByTestId('track-audio-11-2')).toHaveTextContent('—')
    expect(screen.getByTestId('track-status-11-1')).toHaveTextContent('FLAC')
    expect(screen.getByTestId('track-status-11-3')).toHaveTextContent('—')
    expect(client.listTracks).toHaveBeenCalledWith(11)
  })

  it('Expand All opens every group and album', async () => {
    vi.mocked(client.listAlbums).mockResolvedValue([
      album(11, { Name: 'In Rainbows' }),
      album(12, { Name: 'Fake News', AlbumType: 'single' }),
    ])
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-expand-all'))

    expect(await screen.findByTestId('album-11')).toBeVisible()
    expect(screen.getByTestId('album-12')).toBeVisible()
    // Both albums' tracks load lazily on expand.
    await vi.waitFor(() => {
      expect(client.listTracks).toHaveBeenCalledTimes(2)
    })
  })

  it('lists artist history in the history modal', async () => {
    const recent = new Date(Date.now() - 2 * 60 * 1000).toISOString()
    vi.mocked(client.getArtistHistory).mockResolvedValue([
      { Event: 'download', Detail: 'In Rainbows.flac', At: recent },
      { Event: 'added', Detail: 'Artist added', At: recent },
    ] satisfies MusicHistoryEntry[])
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-history'))

    const modal = await screen.findByTestId('history-modal')
    expect(modal).toBeInTheDocument()
    expect(await screen.findByTestId('history-0')).toHaveTextContent('download')
    expect(screen.getByTestId('history-0')).toHaveTextContent('In Rainbows.flac')
    expect(screen.getByTestId('history-0')).toHaveTextContent('2 min ago')
    expect(screen.getByTestId('history-1')).toHaveTextContent('added')
  })

  it('opens the artist monitoring modal with artist + per-album toggles', async () => {
    vi.mocked(client.listAlbums).mockResolvedValue([album(11, { Name: 'In Rainbows' })])
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-monitoring'))

    expect(await screen.findByTestId('monitoring-modal')).toBeInTheDocument()
    expect(screen.getByTestId('monitoring-artist')).toBeInTheDocument()
    expect(await screen.findByTestId('monitoring-album-11')).toHaveTextContent('In Rainbows')
    expect(screen.getByTestId('monitoring-album-mon-11')).toBeInTheDocument()

    await user.click(screen.getByTestId('monitoring-album-mon-11'))
    expect(client.setAlbumMonitored).toHaveBeenCalledWith(11, false)
  })

  it('opens the L2 edit modal with the full field set in Lidarr order', async () => {
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-edit'))

    await screen.findByTestId('edit-modal')
    // Field order: Monitored · Monitor New Albums · Monitor Policy ·
    // Quality Profile · Metadata Profile (L3 stub, disabled) · Path · Tags.
    const order = [
      'edit-monitored',
      'edit-monitor-new-albums',
      'edit-monitor-policy',
      'edit-profile',
      'edit-metadata-profile',
      'edit-path',
      'edit-tags-input',
    ]
    const byPosition = [...document.querySelectorAll('[data-testid^="edit-"]')]
      .map((el) => el.getAttribute('data-testid'))
      .filter((id) => id !== null && id !== 'edit-save' && id !== 'edit-tags-chips')
    expect(
      order.every((id) => byPosition.includes(id)),
    ).toBe(true)
    const seven = byPosition.filter((id) => order.includes(id as (typeof order)[number]))
    expect(seven).toEqual(order)

    // Pre-filled from the artist row (the '' wire form of the monitor
    // fields coerces to the Lidarr default).
    expect(screen.getByTestId('edit-monitored')).toBeChecked()
    expect(screen.getByTestId('edit-monitor-new-albums')).toHaveValue('all_albums')
    expect(screen.getByTestId('edit-monitor-policy')).toHaveValue('all')
    expect(screen.getByTestId('edit-profile')).toHaveValue('FLAC')
    expect(screen.getByTestId('edit-path')).toHaveValue('Music/Radiohead')

    // The 7 monitor options, Lidarr's "Lastest" spelling included.
    const monitor = screen.getByTestId('edit-monitor-policy') as HTMLSelectElement
    expect(Array.from(monitor.options).map((o) => o.label)).toEqual([
      'All Albums',
      'Future Albums',
      'Missing Albums',
      'Existing Albums',
      'First Album',
      'Lastest Album',
      'None',
    ])
    // Metadata profiles land in L3: the select renders disabled with the note.
    expect(screen.getByTestId('edit-metadata-profile')).toBeDisabled()
    expect(screen.getByText(/Metadata profiles land in a later phase/)).toBeInTheDocument()
  })

  it('saves the L2 edit modal fields through the artist update endpoint', async () => {
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-edit'))
    await screen.findByTestId('edit-modal')

    await user.click(screen.getByTestId('edit-monitored')) // un-monitor
    await user.selectOptions(screen.getByTestId('edit-monitor-policy'), 'future')
    await user.selectOptions(screen.getByTestId('edit-profile'), 'MP3 320')
    await user.type(screen.getByTestId('edit-tags-input'), 'lossless{Enter}')
    expect(screen.getByTestId('edit-tags-chips')).toHaveTextContent('lossless')

    await user.click(screen.getByTestId('edit-save'))
    await vi.waitFor(() =>
      expect(client.updateArtist).toHaveBeenCalledWith(7, {
        monitored: false,
        monitor_policy: 'future',
        monitor_new_albums: 'all_albums',
        quality_profile: 'MP3 320',
        path: 'Music/Radiohead',
        tags: ['lossless'],
      }),
    )
    expect(await screen.findByTestId('artist-detail-status')).toHaveTextContent('Artist updated.')
  })

  it('shows the honest empty overview and no-albums states', async () => {
    renderDetail()
    await screen.findByTestId('artist-detail')
    expect(screen.getByTestId('artist-detail-overview')).toHaveTextContent('No overview yet')
    expect(screen.getByTestId('artist-detail-no-albums')).toBeInTheDocument()
    // The helper line only renders once albums exist.
    expect(screen.queryByTestId('artist-detail-helper')).not.toBeInTheDocument()
  })
})
