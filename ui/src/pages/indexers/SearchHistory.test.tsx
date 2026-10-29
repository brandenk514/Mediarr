import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { SearchHistory } from '../../api/types'
import { SearchHistoryPage } from './SearchHistory'

// Mock the typed client (only the two history endpoints are used here).
vi.mock('../../api/client', () => ({
  client: {
    searchHistory: vi.fn(),
    clearSearchHistory: vi.fn(),
  },
}))

const PAGE1: SearchHistory = {
  items: [
    {
      id: 1,
      indexer: 'nyaa',
      term: 'breaking bad',
      categories: ['Anime'],
      ok: true,
      elapsed_ms: 1234,
      at: '2026-10-19T21:45:00Z',
    },
    {
      id: 2,
      indexer: 'btsou',
      term: 'dune',
      categories: [],
      ok: false,
      elapsed_ms: 45,
      at: '2026-10-18T08:00:00Z',
    },
  ],
  total: 30,
}

const PAGE2: SearchHistory = {
  items: [
    {
      id: 3,
      indexer: 'nulled',
      term: 'dune part two',
      categories: [],
      ok: true,
      elapsed_ms: 77,
      at: '2026-10-17T12:00:00Z',
    },
  ],
  total: 30,
}

function renderHistory() {
  return render(
    <MemoryRouter initialEntries={['/history']}>,
      <SearchHistoryPage />
    </MemoryRouter>,
  )
}

function lastCallArgs(): { page: number; page_size: number; indexer?: string; q?: string } {
  const calls = (client.searchHistory as ReturnType<typeof vi.fn>).mock.calls
  return calls[calls.length - 1][0]
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.searchHistory as ReturnType<typeof vi.fn>).mockResolvedValue(PAGE1)
  ;(client.clearSearchHistory as ReturnType<typeof vi.fn>).mockResolvedValue({ deleted: 3 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SearchHistoryPage — rows (plan §2 History)', () => {
  it('renders rows from the seeded page with status, indexer, query, categories, date, elapsed', async () => {
    renderHistory()
    await screen.findByTestId('history-table')
    const row0 = screen.getByTestId('history-row-0')
    expect(row0).toHaveTextContent('nyaa')
    expect(row0).toHaveTextContent('breaking bad')
    expect(screen.getByTestId('history-status-0')).toHaveTextContent('✓')
    expect(row0).toHaveTextContent('Anime')
    expect(row0).toHaveTextContent('1234 ms')
    expect(screen.getByTestId('history-date-0').textContent).toMatch(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}/)
    // Failed row shows ✗ and an empty category cell.
    expect(screen.getByTestId('history-status-1')).toHaveTextContent('✗')
    expect(screen.getByTestId('history-cats-1')).toHaveTextContent('—')
  })

  it('renders the Prowlarr-style footer "Page X · Total records: N" with Next enabled', async () => {
    renderHistory()
    await screen.findByTestId('history-foot')
    expect(screen.getByTestId('history-foot')).toHaveTextContent('Page 1 · Total records: 30')
    expect(screen.getByTestId('history-next')).toBeEnabled()
    expect(screen.getByTestId('history-prev')).toBeDisabled()
  })

  it('shows the empty state when the server returns no rows', async () => {
    ;(client.searchHistory as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [], total: 0 })
    renderHistory()
    await screen.findByTestId('history-empty')
    expect(screen.getByTestId('history-empty')).toHaveTextContent('No search history')
  })
})

describe('SearchHistoryPage — pagination + filters', () => {
  it('Next requests page 2 from the server', async () => {
    const user = userEvent.setup()
    ;(client.searchHistory as ReturnType<typeof vi.fn>).mockImplementation((q?: { page?: number }) =>
      q?.page === 2 ? Promise.resolve(PAGE2) : Promise.resolve(PAGE1),
    )
    renderHistory()
    await screen.findByTestId('history-foot')
    await user.click(screen.getByTestId('history-next'))
    await waitFor(() => expect(screen.getByTestId('history-foot')).toHaveTextContent('Page 2 · Total records: 30'))
    const args = lastCallArgs()
    expect(args.page).toBe(2)
    expect(args.page_size).toBe(25)
    expect(args.indexer).toBeUndefined()
    expect(args.q).toBeUndefined()
  })

  it('filter inputs are applied as indexer/q query params (and reset to page 1)', async () => {
    const user = userEvent.setup()
    ;(client.searchHistory as ReturnType<typeof vi.fn>).mockImplementation((q?: { page?: number; indexer?: string }) =>
      q?.page === 2 ? Promise.resolve(PAGE2) : Promise.resolve(PAGE1),
    )
    renderHistory()
    await screen.findByTestId('history-foot')
    await user.click(screen.getByTestId('history-next'))
    await waitFor(() => expect(screen.getByTestId('history-foot')).toHaveTextContent('Page 2'))
    await user.type(screen.getByTestId('history-filter-indexer'), 'nyaa')
    await waitFor(() => {
      const args = lastCallArgs()
      expect(args.indexer).toBe('nyaa')
      expect(args.page).toBe(1)
    })
    await user.type(screen.getByTestId('history-filter-q'), 'du')
    await waitFor(() => {
      const args = lastCallArgs()
      expect(args.q).toBe('du')
      expect(args.indexer).toBe('nyaa')
    })
  })

  it('Refresh re-requests the current page', async () => {
    const user = userEvent.setup()
    renderHistory()
    await screen.findByTestId('history-refresh')
    const callsAfterLoad = (client.searchHistory as ReturnType<typeof vi.fn>).mock.calls.length
    await user.click(screen.getByTestId('history-refresh'))
    await waitFor(() =>
      expect((client.searchHistory as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(callsAfterLoad),
    )
  })
})

describe('SearchHistoryPage — clear', () => {
  it('Clear asks for confirmation, deletes, shows the count, and reloads', async () => {
    const user = userEvent.setup()
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderHistory()
    await screen.findByTestId('history-clear')
    await user.click(screen.getByTestId('history-clear'))
    await waitFor(() => expect(client.clearSearchHistory).toHaveBeenCalledTimes(1))
    expect(confirmSpy).toHaveBeenCalledWith(expect.stringContaining('Clear all search history'))
    await waitFor(() => expect(screen.getByTestId('history-cleared')).toHaveTextContent('3 cleared'))
  })

  it('Cancel keeps the history (no DELETE issued)', async () => {
    const user = userEvent.setup()
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    renderHistory()
    await screen.findByTestId('history-clear')
    await user.click(screen.getByTestId('history-clear'))
    await new Promise((r) => setTimeout(r, 50))
    expect(client.clearSearchHistory).not.toHaveBeenCalled()
  })
})
