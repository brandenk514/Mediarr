import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../../api/client'
import type { DownloadClientView, LibraryOverrides } from '../../../api/types'
import { DownloadClientsSection } from './DownloadClientsSection'

// Mock the typed client so the section talks to in-test fakes. The section is
// a read-once + save-on-demand view (no live stream), so we only need to stub
// the download-client and library-override methods it calls.
vi.mock('../../../api/client', () => ({
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

function renderSection() {
  return render(
    <MemoryRouter>
      <DownloadClientsSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue(dcView)
  ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockResolvedValue(libOverrides())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DownloadClientsSection', () => {
  it('loads the download-client view and library overrides', async () => {
    renderSection()
    // Download client defaults to the loaded kind.
    expect(await screen.findByTestId('dc-kind')).toHaveValue('mock')
    // Library columns render with their seeded values.
    expect(await screen.findByTestId('lib-movie-path')).toHaveValue('/data/movies')
    // The checksum select seeds from the override in a post-mount effect.
    await waitFor(() => expect(screen.getByTestId('lib-music-checksum')).toHaveValue('false'))
  })

  it('surfaces a fatal error with a retry on load failure', async () => {
    ;(client.getDownloadClient as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'boom'),
    )
    renderSection()
    await waitFor(() =>
      expect(screen.getByTestId('settings-error-message')).toHaveTextContent('boom'),
    )
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })

  it('shows the 401 auth-lost affordance', async () => {
    ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'unauthorized'),
    )
    renderSection()
    await waitFor(() =>
      expect(screen.getByTestId('settings-error-message')).toHaveTextContent('unauthorized'),
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
    renderSection()
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
    ;(client.putLibraryOverride as ReturnType<typeof vi.fn>).mockResolvedValue({
      path: '/x',
      quality_profile: 'HD',
      checksum_verify: null,
    })
    renderSection()
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
})
