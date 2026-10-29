import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { BlocklistEntry } from '../../api/types'
import { BlocklistPage } from './Blocklist'

vi.mock('../../api/client', () => ({
  client: {
    listBlocklist: vi.fn(),
    unblockRelease: vi.fn(),
  },
}))

const NOW = '2026-10-11T12:00:00Z'

function entries(): BlocklistEntry[] {
  return [
    { id: 1, kind: 'tv', parent_id: 10, parent_name: 'Severance', release_title: 'Severance.S01E01.HDTV.x264', indexer: 'nyaa', reason: 'cutoff unmet', album_title: '', quality: '', formats: '', created_at: NOW },
    { id: 2, kind: 'movie', parent_id: 20, parent_name: 'Dune', release_title: 'Dune.Part.Two.1080p.WEB', indexer: 'rarbg', reason: '', album_title: '', quality: '', formats: '', created_at: NOW },
  ]
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/activity/blocklist']}>
      <BlocklistPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listBlocklist).mockReset()
  vi.mocked(client.unblockRelease).mockReset()
})

describe('BlocklistPage', () => {
  it('loads the blocklist and renders one row per entry', async () => {
    vi.mocked(client.listBlocklist).mockResolvedValue(entries())
    renderPage()
    expect(screen.getByText('Loading blocklist…')).toBeInTheDocument()
    await waitFor(() => expect(client.listBlocklist).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('Severance.S01E01.HDTV.x264')).toBeInTheDocument()
    expect(screen.getByText('Dune.Part.Two.1080p.WEB')).toBeInTheDocument()
    // The reason column surfaces the auto-block reason; media column shows the parent name.
    expect(screen.getByTestId('blocklist-reason-1')).toHaveTextContent('cutoff unmet')
    expect(screen.getByTestId('blocklist-row-2')).toHaveTextContent('Dune')
  })

  it('filters by kind and re-queries the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listBlocklist).mockResolvedValue(entries())
    renderPage()
    await waitFor(() => expect(screen.getByTestId('blocklist-kind')).toBeInTheDocument())
    await user.selectOptions(screen.getByTestId('blocklist-kind'), 'tv')
    await waitFor(() =>
      expect(client.listBlocklist).toHaveBeenLastCalledWith({ kind: 'tv' }),
    )
  })

  it('unblocks a row via the API and removes it locally', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listBlocklist).mockResolvedValue(entries())
    vi.mocked(client.unblockRelease).mockResolvedValue(undefined as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('blocklist-unblock-1')).toBeInTheDocument())
    await user.click(screen.getByTestId('blocklist-unblock-1'))
    await waitFor(() => expect(client.unblockRelease).toHaveBeenCalledWith(1))
    await waitFor(() =>
      expect(screen.queryByText('Severance.S01E01.HDTV.x264')).not.toBeInTheDocument(),
    )
  })

  it('shows an empty state when there are no entries', async () => {
    vi.mocked(client.listBlocklist).mockResolvedValue([])
    renderPage()
    expect(await screen.findByTestId('blocklist-empty')).toBeInTheDocument()
  })

  it('shows an error state on load failure', async () => {
    vi.mocked(client.listBlocklist).mockRejectedValue(new Error('boom'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('blocklist-error')).toBeInTheDocument())
    expect(screen.getByTestId('blocklist-error')).toHaveTextContent('boom')
  })
})
