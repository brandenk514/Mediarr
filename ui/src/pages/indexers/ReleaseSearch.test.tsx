import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type { Indexer, SearchRelease } from '../../api/types'
import { ReleaseSearchPage } from './ReleaseSearch'

// Mock the typed client (ApiError is re-created in the factory because the
// page branches on `instanceof ApiError`).
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
      listIndexers: vi.fn(),
      searchReleases: vi.fn(),
      grabSearchRelease: vi.fn(),
    },
  }
})

function indexer(over: Partial<Indexer> = {}): Indexer {
  return {
    id: 1,
    name: 'nyaa',
    kind: 'torrent-rss',
    base_url: 'https://nyaa.si/api',
    enabled: true,
    has_api_key: false,
    last_test: '2026-10-11T09:00:00Z',
    rss: true,
    auto_search: true,
    interactive_search: true,
    privacy: 'public',
    priority: 25,
    sync_profile: 'standard',
    added: '2026-10-01T12:00:00Z',
    categories: ['Anime', 'TV'],
    ...over,
  }
}

// Two enabled indexers (different category sets) + one disabled (must NOT
// appear in the Indexers multi-select).
const INDEXERS: Indexer[] = [
  indexer(),
  indexer({ id: 2, name: 'btsou', kind: 'nzb', base_url: 'https://btsou.io/api', categories: ['Movies'] }),
  indexer({ id: 3, name: 'nulled', kind: 'torznab', base_url: 'https://nulled.run/api', enabled: false, categories: ['TV'] }),
]

const RELEASES: SearchRelease[] = [
  {
    title: 'Breaking Bad S05 [1080p]',
    indexer: 'nyaa',
    size_bytes: 1_536_000_000,
    seeders: 42,
    protocol: 'torrent',
    url: 'magnet:?xt=urn:btih:abc123',
    category: '',
  },
  {
    title: 'Dune Part Two 216',
    indexer: 'btsou',
    size_bytes: 3_221_225_472,
    seeders: 7,
    protocol: 'nzb',
    url: 'https://btsou.io/download.nzb',
    category: '',
  },
]

function renderSearch() {
  return render(
    <MemoryRouter initialEntries={['/search']}>,
      <ReleaseSearchPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue(INDEXERS)
  ;(client.searchReleases as ReturnType<typeof vi.fn>).mockResolvedValue(RELEASES)
  ;(client.grabSearchRelease as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, indexer: 'nyaa', protocol: 'torrent' })
})

describe('ReleaseSearchPage — form (plan §2 Search)', () => {
  it('renders the form with Search disabled while the query is empty', async () => {
    renderSearch()
    expect(screen.getByTestId('release-search-title')).toHaveTextContent('Search')
    const btn = await screen.findByTestId('release-search-btn')
    expect(btn).toBeDisabled()
    // "Search all indexers" is checked by default; the Indexers picker is then
    // disabled (the selection is ignored).
    expect(screen.getByTestId('release-search-all')).toBeChecked()
    expect(screen.getByTestId('release-search-indexers-btn')).toBeDisabled()
  })

  it('the Indexers multi-select lists only enabled indexers; Categories is the union of their categories', async () => {
    const user = userEvent.setup()
    renderSearch()
    await screen.findByTestId('release-search-all')
    await user.click(screen.getByTestId('release-search-all')) // uncheck "search all"
    expect(screen.getByTestId('release-search-indexers-btn')).toBeEnabled()
    await user.click(screen.getByTestId('release-search-indexers-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-indexers-panel')).toBeInTheDocument())
    // Only the two enabled indexers (nulled is disabled -> filtered out).
    expect(screen.getByTestId('release-search-indexers-opt-1')).toBeInTheDocument()
    expect(screen.getByTestId('release-search-indexers-opt-2')).toBeInTheDocument()
    expect(screen.queryByTestId('release-search-indexers-opt-3')).not.toBeInTheDocument()
    // Categories = union {Anime, TV, Movies}, sorted.
    await user.click(screen.getByTestId('release-search-categories-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-categories-panel')).toBeInTheDocument())
    for (const c of ['Anime', 'Movies', 'TV']) {
      expect(screen.getByTestId(`release-search-categories-opt-${c}`)).toBeInTheDocument()
    }
  })

  it('with "Search all indexers" checked, the request omits indexer_ids', async () => {
    const user = userEvent.setup()
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'Breaking Bad')
    await user.click(screen.getByTestId('release-search-btn'))
    await waitFor(() => expect(client.searchReleases).toHaveBeenCalledTimes(1))
    expect(client.searchReleases).toHaveBeenCalledWith({ query: 'Breaking Bad' })
  })

  it('with "Search all" unchecked, the request carries the selected indexer_ids + categories', async () => {
    const user = userEvent.setup()
    renderSearch()
    await screen.findByTestId('release-search-all')
    await user.click(screen.getByTestId('release-search-all')) // uncheck
    await user.click(screen.getByTestId('release-search-indexers-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-indexers-opt-1')).toBeInTheDocument())
    await user.click(screen.getByTestId('release-search-indexers-opt-1')) // nyaa
    await user.click(screen.getByTestId('release-search-categories-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-categories-opt-Anime')).toBeInTheDocument())
    await user.click(screen.getByTestId('release-search-categories-opt-Anime'))
    await user.type(screen.getByTestId('release-search-query'), 'Dune')
    await user.click(screen.getByTestId('release-search-btn'))
    await waitFor(() => expect(client.searchReleases).toHaveBeenCalledTimes(1))
    expect(client.searchReleases).toHaveBeenCalledWith({
      query: 'Dune',
      indexer_ids: [1],
      categories: ['Anime'],
    })
  })
})

describe('ReleaseSearchPage — results + grab', () => {
  it('renders the results table (title, indexer, human size, seeders, protocol)', async () => {
    const user = userEvent.setup()
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'Breaking Bad')
    await user.click(screen.getByTestId('release-search-btn'))
    await screen.findByTestId('release-search-results')
    const row0 = screen.getByTestId('release-row-0')
    expect(row0).toHaveTextContent('Breaking Bad S05 [1080p]')
    expect(screen.getByTestId('release-indexer-0')).toHaveTextContent('nyaa')
    expect(screen.getByTestId('release-seeders-0')).toHaveTextContent('42')
    // 1,536,000,000 bytes -> 1.4 GB.
    expect(row0).toHaveTextContent('1.4 GB')
    expect(row0).toHaveTextContent('torrent')
  })

  it('a grab click posts the row fields and marks the row Grabbed', async () => {
    const user = userEvent.setup()
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'Breaking Bad')
    await user.click(screen.getByTestId('release-search-btn'))
    await screen.findByTestId('release-grab-0')
    await user.click(screen.getByTestId('release-grab-0'))
    await waitFor(() =>
      expect(client.grabSearchRelease).toHaveBeenCalledWith({
        title: 'Breaking Bad S05 [1080p]',
        indexer: 'nyaa',
        protocol: 'torrent',
        url: 'magnet:?xt=urn:btih:abc123',
        size_bytes: 1_536_000_000,
        seeders: 42,
      }),
    )
    await waitFor(() => expect(screen.getByTestId('release-grab-0')).toHaveTextContent('Grabbed ✓'))
  })

  it('shows a 409 grab error under the row when the client cannot handle the protocol', async () => {
    const user = userEvent.setup()
    ;(client.grabSearchRelease as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, "configured client 'sabnzbd' cannot handle protocol 'torrent'"),
    )
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'Breaking Bad')
    await user.click(screen.getByTestId('release-search-btn'))
    await screen.findByTestId('release-grab-0')
    await user.click(screen.getByTestId('release-grab-0'))
    await waitFor(() =>
      expect(screen.getByTestId('release-error-0')).toHaveTextContent(
        "configured client 'sabnzbd' cannot handle protocol 'torrent'",
      ),
    )
  })

  it('shows a 503 grab error under the row when no download client is configured', async () => {
    const user = userEvent.setup()
    ;(client.grabSearchRelease as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(503, 'no download client configured'),
    )
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'Breaking Bad')
    await user.click(screen.getByTestId('release-search-btn'))
    await screen.findByTestId('release-grab-1')
    await user.click(screen.getByTestId('release-grab-1'))
    await waitFor(() =>
      expect(screen.getByTestId('release-error-1')).toHaveTextContent('no download client configured'),
    )
  })

  it('shows the search error state when the fan-out search fails', async () => {
    const user = userEvent.setup()
    ;(client.searchReleases as ReturnType<typeof vi.fn>).mockRejectedValue(new ApiError(500, 'indexer fan-out failed'))
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'anything')
    await user.click(screen.getByTestId('release-search-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-error')).toHaveTextContent('indexer fan-out failed'))
  })

  it('renders an empty state when the search returns no releases', async () => {
    const user = userEvent.setup()
    ;(client.searchReleases as ReturnType<typeof vi.fn>).mockResolvedValue([])
    renderSearch()
    await screen.findByTestId('release-search-btn')
    await user.type(screen.getByTestId('release-search-query'), 'nothing here')
    await user.click(screen.getByTestId('release-search-btn'))
    await waitFor(() => expect(screen.getByTestId('release-search-empty')).toHaveTextContent('No releases found'))
  })
})
