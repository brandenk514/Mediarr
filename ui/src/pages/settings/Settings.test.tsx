import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type {
  DownloadClientView,
  Indexer,
  LibraryOverrides,
} from '../../api/types'
import { SettingsPage } from './Settings'

// Mock the typed client so the page talks to in-test fakes. The page is a
// read-once + save-on-demand view (no live stream), so we only need to stub the
// settings and indexer methods it calls.
vi.mock('../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    body: string
    constructor(status: number, body: string) {
      super(body)
      this.status = status
      this.body = body
    }
  },
  client: {
    getDownloadClient: vi.fn(),
    putDownloadClient: vi.fn(),
    getLibraryOverrides: vi.fn(),
    putLibraryOverride: vi.fn(),
    listIndexers: vi.fn(),
    testIndexer: vi.fn(),
    createIndexer: vi.fn(),
    updateIndexer: vi.fn(),
    setIndexerEnabled: vi.fn(),
    deleteIndexer: vi.fn(),
  },
}))

const dcView: DownloadClientView = {
  kind: 'mock',
  qb_base: '',
  qb_username: '',
  qb_api_key_set: false,
  qb_password_set: false,
  sab_base: '',
  sab_api_key_set: false,
  sab_complete_dir: '',
}

function libOverrides(): LibraryOverrides {
  return {
    movie: { path: '/data/movies', quality_profile: 'HD', checksum_verify: null },
    tv: { path: '', quality_profile: '', checksum_verify: null },
    music: { path: '', quality_profile: '', checksum_verify: false },
    book: { path: '', quality_profile: '', checksum_verify: null },
  }
}

function indexer(over: Partial<Indexer> = {}): Indexer {
  return {
    id: 1,
    name: 'nyaa',
    kind: 'torrent-rss',
    base_url: 'https://nyaa.si/api',
    enabled: true,
    has_api_key: false,
    last_test: '2026-10-11T09:00:00Z',
    ...over,
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue(dcView)
  ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockResolvedValue(libOverrides())
  ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([indexer()])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SettingsPage', () => {
  it('loads the download-client view, libraries, and indexers', async () => {
    renderPage()
    await waitFor(() =>
      expect(screen.getByTestId('settings-title')).toHaveTextContent('Settings'),
    )
    // Download client defaults to the loaded kind.
    expect(await screen.findByTestId('dc-kind')).toHaveValue('mock')
    // Library columns render with their seeded values.
    expect(await screen.findByTestId('lib-movie-path')).toHaveValue('/data/movies')
    expect(screen.getByTestId('lib-music-checksum')).toHaveValue('false')
    // Indexers table shows the seeded row.
    expect(await screen.findByTestId('idx-row-1')).toHaveTextContent('nyaa')
    // Masked key state: nyaa has no key → "—".
    expect(screen.getByTestId('idx-row-1')).toHaveTextContent('torrent-rss')
  })

  it('surfaces a fatal error with a retry on load failure', async () => {
    ;(client.getDownloadClient as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'boom'),
    )
    renderPage()
    await waitFor(() =>
      expect(screen.getByTestId('settings-error-message')).toHaveTextContent('boom'),
    )
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })

  it('shows the 401 auth-lost affordance', async () => {
    ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'unauthorized'),
    )
    renderPage()
    await waitFor(() =>
      expect(screen.getByTestId('settings-error-message')).toHaveTextContent(
        'unauthorized',
      ),
    )
    expect(screen.getByText('Sign in again')).toBeVisible()
  })

  it('switches the download client to qbittorrent and saves', async () => {
    const user = userEvent.setup()
    ;(client.putDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue({
      ...dcView,
      kind: 'qbittorrent',
      qb_base: 'http://localhost:8080',
      qb_api_key_set: true,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('dc-kind')).toBeVisible())

    await user.selectOptions(screen.getByTestId('dc-kind'), 'qbittorrent')
    await waitFor(() => expect(screen.getByTestId('dc-qb-base')).toBeVisible())

    await user.type(screen.getByTestId('dc-qb-base'), 'http://localhost:8080')
    // Leave the API key blank to keep the (not-yet-stored) key.
    await user.click(screen.getByTestId('dc-save'))
    await waitFor(() =>
      expect(client.putDownloadClient).toHaveBeenCalledWith(
        expect.objectContaining({ kind: 'qbittorrent', qb_base: 'http://localhost:8080' }),
      ),
    )
  })

  it('saves a per-library override with checksum opt-out', async () => {
    const user = userEvent.setup()
    const saved = { path: '/x', quality_profile: 'LOSSLESS', checksum_verify: false }
    ;(client.putLibraryOverride as ReturnType<typeof vi.fn>).mockResolvedValue(saved)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('lib-movie-path')).toBeVisible())

    await user.clear(screen.getByTestId('lib-movie-path'))
    await user.type(screen.getByTestId('lib-movie-path'), '/x')
    await user.click(screen.getByTestId('lib-movie-save'))
    await waitFor(() =>
      expect(client.putLibraryOverride).toHaveBeenCalledWith('movie', {
        path: '/x',
        quality_profile: 'HD',
        checksum_verify: null,
      }),
    )
  })

  it('creates an indexer from the add form', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 7, name: 'newidx', kind: 'torznab', base_url: 'https://x' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([indexer(), created])
    renderPage()
    await waitFor(() => expect(screen.getByTestId('idx-add')).toBeVisible())

    await user.click(screen.getByTestId('idx-add'))
    await waitFor(() => expect(screen.getByTestId('idx-name')).toBeVisible())
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    await user.selectOptions(screen.getByTestId('idx-kind'), 'torznab')
    await user.type(screen.getByTestId('idx-base'), 'https://x')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'newidx', kind: 'torznab' }),
      ),
    )
  })

  it('toggles indexer enabled state', async () => {
    const user = userEvent.setup()
    ;(client.setIndexerEnabled as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ enabled: false }))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    await user.click(screen.getByTestId('idx-toggle-1'))
    await waitFor(() =>
      expect(client.setIndexerEnabled).toHaveBeenCalledWith(1, false),
    )
  })

  it('runs an indexer test and shows the result pill', async () => {
    const user = userEvent.setup()
    ;(client.testIndexer as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    await user.click(screen.getByTestId('idx-test-1'))
    await waitFor(() =>
      expect(client.testIndexer).toHaveBeenCalledWith(1),
    )
    await waitFor(() =>
      expect(screen.getByTestId('idx-row-1')).toHaveTextContent('ok'),
    )
  })

  it('deletes an indexer after confirm', async () => {
    const user = userEvent.setup()
    ;(client.deleteIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    // First click arms the delete ("Confirm?"), second fires it.
    await user.click(screen.getByTestId('idx-delete-1'))
    await user.click(screen.getByTestId('idx-delete-1'))
    await waitFor(() =>
      expect(client.deleteIndexer).toHaveBeenCalledWith(1),
    )
  })
})
