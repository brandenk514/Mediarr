import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../api/client'
import type { EventFrame } from '../api/types'
import { DashboardPage } from './Dashboard'

// Capture the callbacks the page wires into connectEvents so a test can push a
// live frame (queue.updated) and assert the dashboard re-reads the snapshot.
let liveOnEvent: ((f: EventFrame) => void) | null = null
let liveClose = vi.fn()

vi.mock('../api/client', () => ({
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
    getDashboard: vi.fn(),
  },
  connectEvents: (
    onEvent: (f: EventFrame) => void,
    _onStatus?: (s: 'connecting' | 'open' | 'closed') => void,
  ) => {
    liveOnEvent = onEvent
    liveClose = vi.fn()
    return liveClose
  },
}))

function sampleDashboard() {
  return {
    libraries: [
      { kind: 'movie', label: 'Movies', count: 3, wants: 1, queue: 2 },
      { kind: 'tv', label: 'TV', count: 2, wants: 0, queue: 0 },
      { kind: 'music', label: 'Music', count: 1, wants: 1, queue: 0 },
      { kind: 'book', label: 'Books', count: 0, wants: 0, queue: 0 },
    ],
    totals: { libraries: 6, wants: 2, queue: 2 },
    recent_history: [{ kind: 'movie', name: 'Dune', event: 'added', detail: 'added to library', at: '2026-10-11T12:00:00Z' }],
    recent_queue: [{ kind: 'movie', release_title: 'Dune.Part.Two.2160p', indexer: 'fake', state: 'downloading', progress: 42, updated_at: '2026-10-11T12:00:05Z' }],
    indexers: [{ name: 'fake', status: 'ok', successes: 5, failures: 0, last_error: '', last_success: '2026-10-11T11:00:00Z', avg_latency_ns: 1_000_000 }],
    generated_at: '2026-10-11T12:00:06Z',
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.getDashboard).mockReset()
  liveOnEvent = null
  liveClose = vi.fn()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('DashboardPage', () => {
  it('loads from the v1 API and renders per-kind cards, indexer health, and feeds', async () => {
    vi.mocked(client.getDashboard).mockResolvedValue(sampleDashboard() as never)
    renderPage()

    // loading state first
    expect(screen.getByText('Loading dashboard…')).toBeInTheDocument()

    await waitFor(() => expect(client.getDashboard).toHaveBeenCalledWith(20))

    // per-kind library cards with their counts
    expect(await screen.findByTestId('lib-movie')).toHaveTextContent('Movies')
    expect(screen.getByTestId('lib-movie')).toHaveTextContent('3')
    expect(screen.getByTestId('lib-movie')).toHaveTextContent('2 in queue')
    expect(screen.getByTestId('lib-book')).toHaveTextContent('Books')

    // indexer health at a glance
    expect(await screen.findByTestId('indexer-fake')).toHaveTextContent('fake')
    expect(screen.getByTestId('indexer-status-fake')).toHaveTextContent('ok')

    // recent queue + history feeds
    expect(screen.getByTestId('queue-feed')).toHaveTextContent('Dune.Part.Two.2160p')
    expect(screen.getByTestId('history-feed')).toHaveTextContent('Dune')
    expect(screen.getByTestId('live-badge')).toBeInTheDocument()
  })

  // Regression: an empty-DB backend used to emit `null` for the recent feeds
  // (Go marshals nil slices as null), which crashed the feed render and
  // unmounted the whole app — a black page right after login. The dashboard
  // must render its empty states instead.
  it('renders empty states when the API emits null for the recent feeds', async () => {
    const d = {
      ...sampleDashboard(),
      recent_history: null,
      recent_queue: null,
    } as never
    vi.mocked(client.getDashboard).mockResolvedValue(d)
    renderPage()

    expect(await screen.findByText('No recent activity.')).toBeInTheDocument()
    expect(screen.getByText('No recent queue activity.')).toBeInTheDocument()
    // the rest of the dashboard is still up
    expect(screen.getByTestId('lib-movie')).toBeInTheDocument()
    expect(screen.getByTestId('live-badge')).toBeInTheDocument()
  })

  it('shows an error state with retry on load failure', async () => {
    vi.mocked(client.getDashboard).mockRejectedValue(new Error('boom'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('dashboard-error')).toBeInTheDocument())
    expect(screen.getByTestId('dashboard-error-message')).toHaveTextContent('boom')
  })

  it('treats a 401 as an auth-expiry state', async () => {
    vi.mocked(client.getDashboard).mockRejectedValue(new ApiError(401, 'authentication expired or invalid'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('dashboard-error')).toBeInTheDocument())
    expect(screen.getByText('Sign in again')).toBeInTheDocument()
  })

  it('re-reads the snapshot when a live queue.updated frame arrives', async () => {
    let callCount = 0
    vi.mocked(client.getDashboard).mockImplementation(async () => {
      callCount += 1
      const d = sampleDashboard()
      // second read reflects the change the frame announced
      if (callCount >= 2) d.recent_queue = [{ kind: 'movie', release_title: 'Dune.Part.Two.2160p', indexer: 'fake', state: 'complete', progress: 100, updated_at: '2026-10-11T12:01:05Z' }]
      return d as never
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-feed')).toBeInTheDocument())

    // push a live frame through the captured WS handler
    liveOnEvent?.({
      type: 'queue.updated',
      payload: {
        movie_id: 101,
        queue_id: 0,
        release_title: 'rel',
        indexer: 'idx',
        client: 'qBittorrent',
        state: 'complete',
        progress: 100,
      },
      at: '2026-10-11T12:01:00Z',
    })

    await waitFor(() => expect(client.getDashboard).toHaveBeenCalledTimes(2))
    // and the feed reflects the updated state
    await waitFor(() => expect(screen.getByTestId('queue-0')).toHaveTextContent('complete'))
  })

  it('retries on the refresh button', async () => {
    const user = userEvent.setup()
    vi.mocked(client.getDashboard).mockResolvedValue(sampleDashboard() as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('refresh')).toBeInTheDocument())
    await user.click(screen.getByTestId('refresh'))
    await waitFor(() => expect(client.getDashboard).toHaveBeenCalledTimes(2))
  })
})
