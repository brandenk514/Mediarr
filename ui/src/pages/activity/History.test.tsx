import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { GlobalHistoryItem } from '../../api/types'
import { HistoryPage } from './History'

vi.mock('../../api/client', () => ({
  client: { listGlobalHistory: vi.fn() },
}))

const NOW = '2026-10-11T12:00:00Z'

function feed(): GlobalHistoryItem[] {
  return [
    { kind: 'tv', parent_id: 10, name: 'Severance', event: 'imported', detail: 'imported S01E02', at: NOW },
    { kind: 'movie', parent_id: 20, name: 'Dune', event: 'search-requested', detail: 'manual search', at: NOW },
    { kind: 'music', parent_id: 30, name: 'Radiohead', event: 'monitored', detail: 'album monitored', at: NOW },
  ]
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/activity/history']}>
      <HistoryPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listGlobalHistory).mockReset()
})

describe('HistoryPage', () => {
  it('loads the global feed and renders one row per event', async () => {
    vi.mocked(client.listGlobalHistory).mockResolvedValue(feed())
    renderPage()
    expect(screen.getByText('Loading history…')).toBeInTheDocument()
    await waitFor(() => expect(client.listGlobalHistory).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('Severance')).toBeInTheDocument()
    expect(screen.getByText('Dune')).toBeInTheDocument()
    // Three lifecycle events render as feed rows.
    expect(screen.getAllByTestId(/^history-item-/)).toHaveLength(3)
  })

  it('filters by event type and re-queries the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listGlobalHistory).mockResolvedValue(feed())
    renderPage()
    await waitFor(() => expect(screen.getByTestId('history-event')).toBeInTheDocument())
    await user.selectOptions(screen.getByTestId('history-event'), 'download')
    await waitFor(() =>
      expect(client.listGlobalHistory).toHaveBeenLastCalledWith(
        expect.objectContaining({ event: 'download', limit: 100 }),
      ),
    )
  })

  it('sends a since date normalised to start-of-day UTC', async () => {
    vi.mocked(client.listGlobalHistory).mockResolvedValue(feed())
    renderPage()
    await waitFor(() => expect(screen.getByTestId('history-since')).toBeInTheDocument())
    fireEvent.change(screen.getByTestId('history-since'), { target: { value: '2026-10-01' } })
    await waitFor(() =>
      expect(client.listGlobalHistory).toHaveBeenLastCalledWith(
        expect.objectContaining({ since: '2026-10-01T00:00:00Z' }),
      ),
    )
  })

  it('shows an empty state when there are no events', async () => {
    vi.mocked(client.listGlobalHistory).mockResolvedValue([])
    renderPage()
    expect(await screen.findByTestId('history-empty')).toBeInTheDocument()
  })

  it('shows an error state with retry on load failure', async () => {
    vi.mocked(client.listGlobalHistory).mockRejectedValue(new Error('boom'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('history-error-message')).toBeInTheDocument())
    expect(screen.getByTestId('history-error-message')).toHaveTextContent('boom')
  })
})
