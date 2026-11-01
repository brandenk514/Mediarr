import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../../api/client'
import type { DownloadClient, LibraryOverrides } from '../../../api/types'
import { DownloadClientsSection } from './DownloadClientsSection'

// Mock the typed client so the section talks to in-test fakes. The section is a
// read-once + save-on-demand view (no live stream), so we stub only the
// plural download-client methods and the library-override methods it calls.
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
    listDownloadClients: vi.fn(),
    createDownloadClient: vi.fn(),
    getDownloadClient: vi.fn(),
    updateDownloadClient: vi.fn(),
    deleteDownloadClient: vi.fn(),
    testDownloadClient: vi.fn(),
    testAllDownloadClients: vi.fn(),
    getLibraryOverrides: vi.fn(),
    putLibraryOverride: vi.fn(),
  },
}))

// A single seeded client: a qBittorrent with a stored API key (api_key is
// always "" on read; api_key_set reports the stored key's presence).
const dc1: DownloadClient = {
  id: 1,
  name: 'qBittorrent',
  kind: 'qbittorrent',
  enabled: true,
  position: 1,
  base_url: 'http://localhost:8080',
  username: 'admin',
  api_key: '',
  api_key_set: true,
  complete_dir: '/downloads',
  created_at: '2026-01-01T00:00:00Z',
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
  ;(client.listDownloadClients as ReturnType<typeof vi.fn>).mockResolvedValue([dc1])
  ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockResolvedValue(libOverrides())
  ;(client.updateDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue(dc1)
  ;(client.createDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue(dc1)
  ;(client.deleteDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
  ;(client.testDownloadClient as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true, message: '' })
  ;(client.testAllDownloadClients as ReturnType<typeof vi.fn>).mockResolvedValue([
    { id: 1, ok: true, message: '' },
  ])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DownloadClientsSection', () => {
  it('shows the empty state when there are no download clients', async () => {
    ;(client.listDownloadClients as ReturnType<typeof vi.fn>).mockResolvedValue([])
    renderSection()
    expect(await screen.findByTestId('dc-empty')).toBeInTheDocument()
    expect(screen.getByTestId('dcl-test-all')).toBeDisabled()
  })

  it('loads the client list and library overrides', async () => {
    renderSection()
    // The seeded client row renders with its name, kind, and enabled state.
    const row = await screen.findByTestId('dcl-row-1')
    expect(row).toBeVisible()
    expect(screen.getByTestId('dcl-name-1')).toHaveTextContent('qBittorrent')
    expect(screen.getByTestId('dcl-kind-1')).toHaveTextContent('qbittorrent')
    expect(screen.getByTestId('dcl-enabled-1')).toHaveTextContent('Enabled')
    // Library columns render with their seeded values.
    expect(screen.getByTestId('lib-movie-path')).toHaveValue('/data/movies')
    // The checksum select seeds from the override in a post-mount effect.
    await waitFor(() => expect(screen.getByTestId('lib-music-checksum')).toHaveValue('false'))
  })

  it('surfaces a fatal error with a retry on load failure', async () => {
    ;(client.listDownloadClients as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'boom'),
    )
    renderSection()
    await waitFor(() =>
      expect(screen.getByTestId('dc-error-message')).toHaveTextContent('boom'),
    )
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
  })

  it('shows the 401 auth-lost affordance', async () => {
    ;(client.getLibraryOverrides as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'unauthorized'),
    )
    renderSection()
    await waitFor(() =>
      expect(screen.getByTestId('dc-error-message')).toHaveTextContent('unauthorized'),
    )
    expect(screen.getByText('Sign in again')).toBeVisible()
  })

  it('adds a client (sabnzbd) with the base url', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('dcl-add')

    await user.click(screen.getByTestId('dcl-add'))
    await user.type(await screen.findByTestId('dcl-f-name'), 'SAB')
    await user.selectOptions(screen.getByTestId('dcl-f-kind'), 'sabnzbd')
    await user.type(screen.getByTestId('dcl-f-base'), 'http://localhost:11111')
    await user.click(screen.getByTestId('dcl-save'))

    await waitFor(() =>
      expect(client.createDownloadClient).toHaveBeenCalledWith({
        name: 'SAB',
        kind: 'sabnzbd',
        enabled: true,
        base_url: 'http://localhost:11111',
        username: '',
        api_key: '',
        complete_dir: '',
      }),
    )
  })

  it('edits a client and keeps the stored key (masked round-trip)', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('dcl-edit-1')

    await user.click(screen.getByTestId('dcl-edit-1'))
    // The form seeds from the client; a stored key shows the keep-blank hint.
    await screen.findByTestId('dcl-f-key')
    fireEvent.change(screen.getByTestId('dcl-f-name'), { target: { value: 'Renamed' } })
    await user.click(screen.getByTestId('dcl-save'))

    // api_key is "" (blank = keep the stored key); the rest round-trips.
    await waitFor(() =>
      expect(client.updateDownloadClient).toHaveBeenCalledWith(1, {
        name: 'Renamed',
        kind: 'qbittorrent',
        enabled: true,
        base_url: 'http://localhost:8080',
        username: 'admin',
        api_key: '',
        complete_dir: '/downloads',
      }),
    )
  })

  it('tests a single client and shows the result', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('dcl-test-1')
    await user.click(screen.getByTestId('dcl-test-1'))
    await waitFor(() => expect(screen.getByTestId('dcl-last-test-1')).toHaveTextContent('ok'))
    expect(client.testDownloadClient).toHaveBeenCalledWith(1)
  })

  it('tests all clients from the toolbar', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('dcl-test-all')
    await user.click(screen.getByTestId('dcl-test-all'))
    await waitFor(() => expect(screen.getByTestId('dcl-last-test-1')).toHaveTextContent('ok'))
    expect(client.testAllDownloadClients).toHaveBeenCalled()
  })

  it('deletes a client', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('dcl-del-1')
    await user.click(screen.getByTestId('dcl-del-1'))
    await waitFor(() => expect(client.deleteDownloadClient).toHaveBeenCalledWith(1))
  })

  it('saves a per-library override with checksum opt-out', async () => {
    const user = userEvent.setup()
    ;(client.putLibraryOverride as ReturnType<typeof vi.fn>).mockResolvedValue({
      path: '/x',
      quality_profile: 'HD',
      checksum_verify: null,
    })
    renderSection()
    await screen.findByTestId('lib-movie-path')

    // fireEvent.change is deterministic: userEvent's clear() intermittently
    // leaves the seeded value in place (then type appends to it).
    fireEvent.change(screen.getByTestId('lib-movie-path'), { target: { value: '/x' } })
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
