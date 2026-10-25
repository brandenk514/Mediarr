import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { MetadataSourceSection } from './MetadataSourceSection'

// Mock the typed client; the source lists ride the per-section document
// endpoint and the test-search box rides the P5.7 chain endpoint.
vi.mock('../../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    body: string
    constructor(status: number, body = '') {
      super(`request failed (${status})`)
      this.status = status
      this.body = body
    }
  },
  client: {
    getSettingsSection: vi.fn(),
    putSettingsSection: vi.fn(),
    searchMetadataSource: vi.fn(),
  },
}))

const FRESH = {
  sources: {
    tv: ['tvdb', 'tvmaze', 'tmdb'],
    movie: ['tmdb'],
    music: ['musicbrainz'],
    book: ['openlibrary'],
  },
}

function renderSection() {
  return render(
    <MemoryRouter>
      <MetadataSourceSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue(FRESH)
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_s: string, doc: unknown) => doc as never,
  )
  ;(client.searchMetadataSource as ReturnType<typeof vi.fn>).mockImplementation(async (req: {
    kind: string
  }) => ({
    kind: req.kind,
    source: 'tvmaze',
    results: [{ id: '1', title: 'Breaking Bad', year: 2008, extra: {} }],
    attempts: [
      { source: 'tvdb', ok: false, latency_ms: 12, error: 'api key not configured' },
      { source: 'tvmaze', ok: true, latency_ms: 30, error: '' },
    ],
  }))
})

describe('MetadataSourceSection (P5.7)', () => {
  it('renders the per-kind ordered source lists', async () => {
    renderSection()
    expect(await screen.findByTestId('ms-src-tv-0')).toHaveTextContent('tvdb (keyed)')
    expect(screen.getByTestId('ms-src-tv-1')).toHaveTextContent('tvmaze (keyless)')
    expect(screen.getByTestId('ms-src-movie-0')).toHaveTextContent('tmdb (keyed)')
    expect(screen.getByTestId('ms-src-book-0')).toHaveTextContent('openlibrary (keyless)')
  })

  it('reorders a source list and saves the new order', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('ms-src-tv-0')
    // Move tvdb down one -> tvmaze becomes the primary source.
    await user.click(screen.getByTestId('ms-down-tv-0'))
    expect(screen.getByTestId('ms-src-tv-0')).toHaveTextContent('tvmaze')
    await user.click(screen.getByTestId('ms-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('metadata-source', {
        sources: {
          movie: ['tmdb'],
          tv: ['tvmaze', 'tvdb', 'tmdb'],
          music: ['musicbrainz'],
          book: ['openlibrary'],
        },
      }),
    )
  })

  it('adds and removes a source in a kind list', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('ms-src-movie-0')
    await user.selectOptions(screen.getByTestId('ms-add-movie'), 'wikidata')
    expect(await screen.findByTestId('ms-src-movie-1')).toHaveTextContent('wikidata')
    await user.click(screen.getByTestId('ms-del-movie-1'))
    expect(screen.queryByTestId('ms-src-movie-1')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('ms-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'metadata-source',
        expect.objectContaining({
          sources: expect.objectContaining({ movie: ['tmdb'] }),
        }),
      ),
    )
  })

  it('runs the chain search and shows the winner + attempt log', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('ms-src-tv-0')
    await user.type(screen.getByTestId('ms-search-query'), 'Breaking')
    await user.click(screen.getByTestId('ms-search-run'))
    await vi.waitFor(() =>
      expect(client.searchMetadataSource).toHaveBeenCalledWith({ kind: 'tv', query: 'Breaking' }),
    )
    expect(await screen.findByTestId('ms-chain-source')).toHaveTextContent('tvmaze')
    expect(screen.getByTestId('ms-chain-hit-0')).toHaveTextContent('Breaking Bad')
    expect(screen.getByTestId('ms-attempt-tvdb')).toHaveTextContent('api key not configured')
    expect(screen.getByTestId('ms-attempt-tvmaze')).toHaveTextContent('ok 30ms')
  })
})
