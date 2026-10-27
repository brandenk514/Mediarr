import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { ArtistSearchResult } from '../../api/types'
import { ArtistAddPage } from './ArtistAdd'

// Page test for the Lidarr L2 Add Artist flow: the debounced MusicBrainz
// search with the two row kinds (artist rows + release-group rows), the
// already-added jump link, and the "Add New Artist" modal (Root Folder →
// Monitor → Monitor New Albums → Quality Profile → Tags, the start-search
// checkbox, and the green add button that also fires the per-artist
// pipeline).
vi.mock('../../api/client', () => {
  class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  }
  return {
    ApiError,
    client: {
      searchArtists: vi.fn(),
      listRootFolders: vi.fn(),
      getProfiles: vi.fn(),
      addArtist: vi.fn(),
      runMusicPipeline: vi.fn(),
    },
  }
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/music/add']}>
      <Routes>
        <Route path="/music/add" element={<ArtistAddPage />} />
        <Route path="/music/:id" element={<div data-testid="artist-detail-landing">Artist detail</div>} />
        <Route path="/music" element={<div data-testid="music-landing">Artists</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const artistHit = {
  id: '52615704-c35c-475d-bcd0-3b4ba1a4e151',
  title: 'Radiohead',
  type: 'Group',
  score: 98,
  country: 'GB',
  disambiguation: 'Oxford band',
  genres: ['Alternative', 'Experimental'],
  links: ['https://www.wikipedia.org/wiki/Radiohead'],
  already_added: false,
}

const addedHit = {
  id: '9e297a16-7cdf-3bc6-91a4-1b60b17ed1da',
  title: 'Tool',
  type: 'Group',
  score: 0, // no popularity data: the heart hides
  country: 'US',
  disambiguation: '',
  genres: [],
  links: [],
  already_added: true,
  library_id: 42,
}

const groupHit = {
  id: 'rg-1',
  title: 'In Rainbows',
  year: 2007,
  release_count: 14,
  type: 'album' as const,
  score: 0,
  artist: 'Radiohead',
}

function searchResult(over: Partial<ArtistSearchResult> = {}): ArtistSearchResult {
  return {
    query: 'radiohead',
    artists: [artistHit, addedHit],
    release_groups: [groupHit],
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.searchArtists).mockResolvedValue(searchResult() as never)
  vi.mocked(client.listRootFolders).mockResolvedValue([
    { ID: 3, Path: '/media/music', Name: 'Music', CreatedAt: '', FreeSpace: 500 * 1024 ** 3 },
  ] as never)
  vi.mocked(client.getProfiles).mockResolvedValue({
    profiles: ['FLAC', 'MP3 320'],
    default: 'FLAC',
  } as never)
  vi.mocked(client.addArtist).mockResolvedValue({ id: 71 })
  vi.mocked(client.runMusicPipeline).mockResolvedValue({ imported: '' } as never)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('ArtistAddPage (L2 search page)', () => {
  it('debounces the search and renders artist rows with heart, type, country and overview', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'radiohead')
    const row = await screen.findByTestId('artist-result-52615704-c35c-475d-bcd0-3b4ba1a4e151')
    expect(row).toHaveTextContent('Radiohead')
    expect(row).toHaveTextContent('♥ 98%')
    expect(row).toHaveTextContent('Group')
    expect(row).toHaveTextContent('GB')
    // Overview = disambiguation + genres joined.
    expect(row).toHaveTextContent('Oxford band · Alternative · Experimental')
  })

  it('hides the heart when the score is unknown (0)', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'tool')
    const row = await screen.findByTestId('artist-result-9e297a16-7cdf-3bc6-91a4-1b60b17ed1da')
    expect(row).not.toHaveTextContent('♥')
  })

  it('renders release-group rows with year, release count, type chip and marker', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'radiohead')
    const row = await screen.findByTestId('release-group-rg-1')
    expect(row).toHaveTextContent('In Rainbows')
    expect(row).toHaveTextContent('(2007)')
    expect(row).toHaveTextContent('14 releases')
    expect(row).toHaveTextContent('album')
    expect(screen.getByTestId('release-group-marker-rg-1')).toHaveTextContent('release group')
  })

  it('renders an already-added artist as a jump link to its detail page', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'tool')
    const link = await screen.findByTestId('artist-result-9e297a16-7cdf-3bc6-91a4-1b60b17ed1da')
    expect(link).toHaveAttribute('href', '/music/42')
    expect(link).toHaveTextContent('In library')
  })

  it('passes the lidarr: MusicBrainz-id lookup straight through to the search', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'lidarr:52615704-c35c-475d-bcd0-3b4ba1a4e151')
    await waitFor(() =>
      expect(client.searchArtists).toHaveBeenCalledWith('lidarr:52615704-c35c-475d-bcd0-3b4ba1a4e151'),
    )
  })

  it('greys out the search on a keyless 503', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.searchArtists).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(503, 'musicbrainz is unavailable'),
    )
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'radiohead')
    expect(await screen.findByTestId('artist-search-unavailable')).toHaveTextContent(
      'musicbrainz is unavailable',
    )
    expect(screen.getByTestId('artist-search-input')).toBeDisabled()
  })
})

describe('AddArtistModal (L2 "Add New Artist")', () => {
  async function openModal(user: ReturnType<typeof userEvent.setup>) {
    renderPage()
    await user.type(screen.getByTestId('artist-search-input'), 'radiohead')
    await user.click(await screen.findByTestId('artist-result-52615704-c35c-475d-bcd0-3b4ba1a4e151'))
    return screen.findByTestId('add-artist-modal')
  }

  it('opens with the Lidarr field order, the 7 monitor options and the root-folder note', async () => {
    const user = userEvent.setup()
    await openModal(user)
    await screen.findByTestId('add-artist-modal')

    expect(screen.getByTestId('add-artist-name')).toHaveTextContent('Radiohead')
    // Field order: Root Folder → Monitor → Monitor New Albums → Quality
    // Profile → Tags (assert via the document order of the labelled
    // controls; all add-artist-* testids are in the same DOM subtree).
    const order = [
      'add-artist-root-folder',
      'add-artist-monitor-policy',
      'add-artist-monitor-new-albums',
      'add-artist-quality-profile',
      'add-artist-tags-input',
    ]
    const byPosition = [...document.querySelectorAll('[data-testid^="add-artist-"]')]
      .map((el) => el.getAttribute('data-testid'))
      .filter((id) => id !== null)
    expect(
      order.every((id) => byPosition.includes(id)),
    ).toBe(true)
    // And the document order of the five controls matches the Lidarr order.
    const five = byPosition.filter((id) => order.includes(id as (typeof order)[number]))
    expect(five).toEqual(order)

    // The 7 monitor options, in Lidarr's labels (including "Lastest").
    const monitor = screen.getByTestId('add-artist-monitor-policy') as HTMLSelectElement
    expect(Array.from(monitor.options).map((o) => o.label)).toEqual([
      'All Albums',
      'Future Albums',
      'Missing Albums',
      'Existing Albums',
      'First Album',
      'Lastest Album',
      'None',
    ])
    expect(monitor).toHaveValue('all')
    expect(screen.getByTestId('add-artist-monitor-policy-help')).toHaveTextContent(
      'Which albums should be monitored after the artist is added (one-time adjustment)',
    )

    // Monitor New Albums: single option with its helper.
    const newAlbums = screen.getByTestId('add-artist-monitor-new-albums') as HTMLSelectElement
    expect(Array.from(newAlbums.options).map((o) => o.label)).toEqual(['All Albums'])
    expect(screen.getByTestId('add-artist-monitor-new-albums-help')).toHaveTextContent(
      'Which new albums should be monitored',
    )

    // Root folder: the bracketed library label + path, the free-space note,
    // and the automatic-subfolder helper.
    const rootFolder = screen.getByTestId('add-artist-root-folder') as HTMLSelectElement
    expect(rootFolder.options[0]!.textContent).toContain('[Music] /media/music')
    expect(screen.getByTestId('add-artist-root-folder-help')).toHaveTextContent(
      "'Radiohead' subfolder will be created automatically",
    )

    // Quality profile: the music profiles with the default preselected.
    const profile = screen.getByTestId('add-artist-quality-profile') as HTMLSelectElement
    await waitFor(() => expect(profile).toHaveValue('FLAC'))
    expect(Array.from(profile.options).map((o) => o.value)).toEqual(['FLAC', 'MP3 320'])
  })

  it('adds with defaults (all / all_albums / default profile) and navigates to the detail page', async () => {
    const user = userEvent.setup()
    await openModal(user)
    await screen.findByTestId('artist-add-button')
    await user.click(screen.getByTestId('artist-add-button'))
    await waitFor(() =>
      expect(client.addArtist).toHaveBeenCalledWith({
        name: 'Radiohead',
        mbid: '52615704-c35c-475d-bcd0-3b4ba1a4e151',
        monitor_policy: 'all',
        monitor_new_albums: 'all_albums',
        quality_profile: 'FLAC',
      }),
    )
    // No start-search → no pipeline.
    expect(client.runMusicPipeline).not.toHaveBeenCalled()
    expect(await screen.findByTestId('artist-detail-landing')).toBeInTheDocument()
  })

  it('submits chosen fields (policy, tags chips) and fires the pipeline when start-search is checked', async () => {
    const user = userEvent.setup()
    await openModal(user)
    await screen.findByTestId('artist-add-button')
    await user.selectOptions(screen.getByTestId('add-artist-monitor-policy'), 'missing')
    await user.type(screen.getByTestId('add-artist-tags-input'), '4k{Enter}')
    expect(screen.getByTestId('add-artist-tags-chips')).toHaveTextContent('4k')
    await user.click(screen.getByTestId('artist-start-search'))
    await user.click(screen.getByTestId('artist-add-button'))
    await waitFor(() =>
      expect(client.addArtist).toHaveBeenCalledWith({
        name: 'Radiohead',
        mbid: '52615704-c35c-475d-bcd0-3b4ba1a4e151',
        monitor_policy: 'missing',
        monitor_new_albums: 'all_albums',
        quality_profile: 'FLAC',
        tags: ['4k'],
      }),
    )
    await waitFor(() => expect(client.runMusicPipeline).toHaveBeenCalledWith(71))
    expect(await screen.findByTestId('artist-detail-landing')).toBeInTheDocument()
  })

  it('surfaces the 409 duplicate in the modal', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.addArtist).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(409, 'artist already exists'),
    )
    const user = userEvent.setup()
    await openModal(user)
    await user.click(await screen.findByTestId('artist-add-button'))
    expect(await screen.findByTestId('add-artist-error')).toHaveTextContent('artist already exists')
  })
})
