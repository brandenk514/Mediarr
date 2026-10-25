import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type { EventFrame, PageQueueItem, QueueViewItem } from '../../api/types'
import { QueuePage } from './Queue'

// Capture the callbacks the page wires into connectEvents so a test can push a
// live queue.updated frame and assert the page re-reads the list.
let liveOnEvent: ((f: EventFrame) => void) | null = null
let liveClose = vi.fn()

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
    listQueue: vi.fn(),
    queueHistory: vi.fn(),
    grabQueue: vi.fn(),
    removeQueue: vi.fn(),
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

const NOW = '2026-10-11T12:00:00Z'

function item(
  kind: QueueViewItem['kind'],
  parentId: number,
  parentName: string,
  release: string,
  state: QueueViewItem['state'],
): QueueViewItem {
  return {
    kind,
    parent_id: parentId,
    parent_name: parentName,
    release_title: release,
    indexer: 'nyaa',
    state,
    progress: state === 'downloading' ? 42 : state === 'complete' ? 100 : 0,
    download_client: state === 'queued' ? '' : 'qBittorrent',
    updated_at: NOW,
  }
}

function samplePage(): PageQueueItem {
  return {
    items: [
      item('movie', 101, 'Dune', 'Dune.Part.Two.2160p.WEB.x265', 'downloading'),
      item('tv', 201, 'Severance', 'Severance.S01E01.1080p', 'queued'),
      item('music', 301, 'Radiohead', 'In.Rainbows.1994.flac', 'complete'),
    ],
    total: 3,
    page: 1,
    page_size: 25,
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <QueuePage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listQueue).mockReset()
  vi.mocked(client.queueHistory).mockReset()
  vi.mocked(client.grabQueue).mockReset()
  vi.mocked(client.removeQueue).mockReset()
  liveOnEvent = null
  liveClose = vi.fn()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('QueuePage', () => {
  it('loads the full queue from the v1 API and renders one row per job', async () => {
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    renderPage()

    expect(screen.getByText('Loading queue…')).toBeInTheDocument()
    await waitFor(() =>
      expect(client.listQueue).toHaveBeenCalledWith(
        expect.objectContaining({ page: 1, page_size: 25 }),
      ),
    )

    // Each job renders as a row with its release + media name.
    expect(await screen.findByTestId('queue-row-movie-101')).toHaveTextContent('Dune.Part.Two.2160p.WEB.x265')
    expect(screen.getByTestId('queue-media-movie-101')).toHaveTextContent('Dune')
    expect(screen.getByTestId('queue-row-tv-201')).toHaveTextContent('Severance.S01E01.1080p')
    expect(screen.getByTestId('queue-row-music-301')).toHaveTextContent('In.Rainbows.1994.flac')

    // The job count and the live badge are present.
    expect(screen.getByTestId('queue-count')).toHaveTextContent('3 jobs')
    expect(screen.getByTestId('live-badge')).toBeInTheDocument()
  })

  it('renders an empty state when no jobs match', async () => {
    vi.mocked(client.listQueue).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 } as never)
    renderPage()
    expect(await screen.findByTestId('queue-empty')).toBeInTheDocument()
  })

  it('shows an error state with retry on load failure', async () => {
    vi.mocked(client.listQueue).mockRejectedValue(new Error('boom'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-error')).toBeInTheDocument())
    expect(screen.getByTestId('queue-error-message')).toHaveTextContent('boom')
  })

  it('treats a 401 as an auth-expiry state', async () => {
    vi.mocked(client.listQueue).mockRejectedValue(new ApiError(401, 'authentication expired or invalid'))
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-error')).toBeInTheDocument())
    expect(screen.getByText('Sign in again')).toBeInTheDocument()
  })

  it('passes the kind + state + search filters to the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-table')).toBeInTheDocument())

    // Filter to the tv kind.
    await user.click(screen.getByTestId('queue-kind-tv'))
    await waitFor(() =>
      expect(client.listQueue).toHaveBeenLastCalledWith(
        expect.objectContaining({ kind: 'tv' }),
      ),
    )

    // Then narrow to queued.
    await user.click(screen.getByTestId('queue-state-queued'))
    await waitFor(() =>
      expect(client.listQueue).toHaveBeenLastCalledWith(
        expect.objectContaining({ kind: 'tv', state: 'queued' }),
      ),
    )

    // Clear the state filter, then search is debounced into `q`.
    await user.click(screen.getByTestId('queue-state-all'))
    await user.type(screen.getByTestId('queue-search'), 'severance')
    await waitFor(() =>
      expect(client.listQueue).toHaveBeenLastCalledWith(
        expect.objectContaining({ q: 'severance' }),
      ),
    )
  })

  it('expands a row and loads its parent history via the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    vi.mocked(client.queueHistory).mockResolvedValue([
      { event: 'added', detail: 'movie added to library', at: NOW },
      { event: 'imported', detail: 'imported to /media/movies/Dune', at: NOW },
    ] as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-expand-movie-101')).toBeInTheDocument())

    await user.click(screen.getByTestId('queue-expand-movie-101'))
    await waitFor(() =>
      expect(client.queueHistory).toHaveBeenCalledWith('movie', 101, 50),
    )
    // Both history events render in the drill-down panel.
    const panel = await screen.findByTestId('queue-history-movie-101')
    expect(panel).toHaveTextContent('movie added to library')
    expect(panel).toHaveTextContent('imported to /media/movies/Dune')
  })

  it('renders an empty-history note when a parent has no events', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    vi.mocked(client.queueHistory).mockResolvedValue([] as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-expand-movie-101')).toBeInTheDocument())
    await user.click(screen.getByTestId('queue-expand-movie-101'))
    const panel = await screen.findByTestId('queue-history-movie-101')
    expect(panel).toHaveTextContent('No history for Dune yet.')
  })

  it('re-reads the queue when a live queue.updated frame arrives', async () => {
    let callCount = 0
    vi.mocked(client.listQueue).mockImplementation(async () => {
      callCount += 1
      const p = samplePage()
      // Second read reflects the change the frame announced.
      if (callCount >= 2) p.items = [item('movie', 101, 'Dune', 'Dune.Part.Two.2160p.WEB.x265', 'complete')]
      return p as never
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-row-movie-101')).toBeInTheDocument())

    liveOnEvent?.({
      type: 'queue.updated',
      payload: {
        movie_id: 101,
        queue_id: 1,
        release_title: 'Dune.Part.Two.2160p.WEB.x265',
        indexer: 'idx',
        client: 'qBittorrent',
        state: 'complete',
        progress: 100,
      },
      at: NOW,
    })

    await waitFor(() => expect(client.listQueue).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByTestId('queue-row-movie-101')).toHaveTextContent('complete'))
  })

  it('retries on the refresh button', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-refresh')).toBeInTheDocument())
    await user.click(screen.getByTestId('queue-refresh'))
    await waitFor(() => expect(client.listQueue).toHaveBeenCalledTimes(2))
  })

  // ---- P3: client column + Grab/Remove Selected ----

  it('renders the download-client column', async () => {
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-table')).toBeInTheDocument())
    expect(screen.getByTestId('queue-client-movie-101')).toHaveTextContent('qBittorrent')
    expect(screen.getByTestId('queue-client-tv-201')).toHaveTextContent('—')
  })

  it('grabs the selected parent via the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    vi.mocked(client.grabQueue).mockResolvedValue({ grabbed: 1 } as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-table')).toBeInTheDocument())
    await user.click(screen.getByTestId('queue-check-movie-101'))
    await user.click(screen.getByTestId('queue-grab'))
    await waitFor(() => expect(client.grabQueue).toHaveBeenCalledWith('movie', 101))
  })

  it('removes the selected parent via the API', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listQueue).mockResolvedValue(samplePage() as never)
    vi.mocked(client.removeQueue).mockResolvedValue({ removed: 1 } as never)
    renderPage()
    await waitFor(() => expect(screen.getByTestId('queue-table')).toBeInTheDocument())
    await user.click(screen.getByTestId('queue-check-tv-201'))
    await user.click(screen.getByTestId('queue-remove'))
    await waitFor(() => expect(client.removeQueue).toHaveBeenCalledWith('tv', 201))
  })
})
