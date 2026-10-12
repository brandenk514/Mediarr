import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Album, Artist, Page, Track } from '../../api/types'
import { MusicPage } from './Music'

// Page test for the music list (#41): paginated artists with two-level lazy
// expansion (artist → album → track).
vi.mock('../../api/client', () => ({
  client: {
    listArtists: vi.fn(),
    listAlbums: vi.fn(),
    listTracks: vi.fn(),
    setArtistMonitored: vi.fn(),
    setAlbumMonitored: vi.fn(),
    setTrackMonitored: vi.fn(),
  },
}))

function artistPage(items: Artist[], total = items.length, pg = 1): Page<Artist> {
  return { items, total, page: pg, page_size: 25 }
}

function artist(id: number, name: string, monitored = true): Artist {
  return { ID: id, MBID: `mbid-${id}`, Name: name, Monitored: monitored, AddedAt: '' }
}

function album(id: number, artistId: number, name: string): Album {
  return { ID: id, ArtistID: artistId, MBID: `alb-${id}`, Name: name, Year: 2019, QualityProfile: 'lossless', Monitored: true, VerifyChecksums: false, Checksum: '', AddedAt: '' }
}

function track(id: number, albumId: number, number: number): Track {
  return { ID: id, AlbumID: albumId, MBID: `trk-${id}`, Disc: 1, Number: number, Title: `Track ${number}`, Checksum: '', Monitored: true, AddedAt: '' }
}

beforeEach(() => {
  vi.mocked(client.listArtists).mockReset()
  vi.mocked(client.listAlbums).mockReset()
  vi.mocked(client.listTracks).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MusicPage', () => {
  it('lazily expands artist → album → track, fetching each level on demand', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(1, 'Radiohead')]))
    vi.mocked(client.listAlbums).mockResolvedValue([album(11, 1, 'In Rainbows')])
    vi.mocked(client.listTracks).mockResolvedValue([track(111, 11, 1), track(112, 11, 2)])
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )

    await screen.findByTestId('artist-1')

    // No child fetches until expanded.
    expect(client.listAlbums).not.toHaveBeenCalled()
    expect(client.listTracks).not.toHaveBeenCalled()

    // Level 1: expand the artist → fetch albums.
    await user.click(screen.getByTestId('artist-toggle-1'))
    expect(await screen.findByTestId('albums-1')).toBeInTheDocument()
    expect(client.listAlbums).toHaveBeenCalledWith(1)
    expect(screen.getByTestId('album-toggle-11')).toBeInTheDocument()

    // Level 2: expand the album → fetch tracks.
    await user.click(screen.getByTestId('album-toggle-11'))
    expect(await screen.findByTestId('tracks-11')).toBeInTheDocument()
    expect(client.listTracks).toHaveBeenCalledWith(11)
    expect(screen.getByText('Track 1')).toBeInTheDocument()
    expect(screen.getByText('Track 2')).toBeInTheDocument()
  })

  it('shows an empty message when an artist has no albums', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(2, 'New Artist')]))
    vi.mocked(client.listAlbums).mockResolvedValue([])
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-2')
    await user.click(screen.getByTestId('artist-toggle-2'))
    expect(await screen.findByText('No albums yet.')).toBeInTheDocument()
  })

  it('toggles an album monitored optimistically without refetching', async () => {
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(1, 'Radiohead')]))
    vi.mocked(client.listAlbums).mockResolvedValue([album(11, 1, 'In Rainbows')])
    vi.mocked(client.setAlbumMonitored).mockResolvedValue({ id: 11, monitored: false })
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MusicPage />
      </MemoryRouter>,
    )
    await screen.findByTestId('artist-1')
    await user.click(screen.getByTestId('artist-toggle-1'))
    await screen.findByTestId('album-mon-11')

    await user.click(screen.getByTestId('album-mon-11'))
    expect(client.setAlbumMonitored).toHaveBeenCalledWith(11, false)
    // Toggling a child does NOT refetch the top-level list.
    expect(client.listArtists).toHaveBeenCalledTimes(1)
  })
})
