import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Indexer, IndexerTestResult } from '../../api/types'
import { IndexerHubPage } from './IndexerHub'

// Mock the typed client so the hub talks to in-test fakes.
vi.mock('../../api/client', () => ({
  client: {
    listIndexers: vi.fn(),
    testIndexer: vi.fn(),
    testAllIndexers: vi.fn(),
    createIndexer: vi.fn(),
    updateIndexer: vi.fn(),
    setIndexerEnabled: vi.fn(),
    deleteIndexer: vi.fn(),
  },
}))

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
    categories: ['Movies', 'TV'],
    ...over,
  }
}
function tResult(id: number, ok: boolean): IndexerTestResult {
  return {
    id,
    name: 'x',
    kind: 'fake',
    base_url: 'https://x',
    enabled: true,
    ok,
    duration: 1,
    at: '2026-10-11T09:00:00Z',
  }
}

// Seed: a torrent (nyaa, public, enabled), a usenet (btsou, private, enabled),
// and a disabled semi-private torznab (nulled). Covers all row states + pills.
const SEED = [
  indexer(),
  indexer({ id: 2, name: 'btsou', kind: 'nzb', privacy: 'private', priority: 10, categories: [], added: '2026-09-15T00:00:00Z' }),
  indexer({ id: 3, name: 'nulled', kind: 'torznab', privacy: 'semi-private', priority: 50, categories: ['TV'], enabled: false, added: '2026-08-20T00:00:00Z' }),
]

function renderHub() {
  return render(
    <MemoryRouter initialEntries={['/indexers']}>
      <IndexerHubPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue(SEED)
  ;(client.testAllIndexers as ReturnType<typeof vi.fn>).mockResolvedValue(SEED.map((i) => tResult(i.id, true)))
})
afterEach(() => {
  vi.restoreAllMocks()
})

describe('IndexerHubPage — list parity (plan §2)', () => {
  it('renders the table with status icon, protocol + privacy pills, priority, sync, added, categories', async () => {
    renderHub()
    await screen.findByTestId('idx-row-1')
    // Title + row.
    expect(screen.getByTestId('idx-hub-title')).toHaveTextContent('Indexers')
    expect(screen.getByTestId('idx-row-1')).toHaveTextContent('nyaa')
    // Protocol pill (green): nyaa = torrent-rss -> torrent.
    expect(screen.getByTestId('idx-proto-1')).toHaveTextContent('torrent')
    expect(screen.getByTestId('idx-proto-2')).toHaveTextContent('usenet')
    // Privacy pills: public / private / semi-private.
    expect(screen.getByTestId('idx-priv-1')).toHaveTextContent('Public')
    expect(screen.getByTestId('idx-priv-2')).toHaveTextContent('Private')
    expect(screen.getByTestId('idx-priv-3')).toHaveTextContent('Semi-Private')
    // Priority + sync profile + added (YYYY-MM-DD).
    expect(screen.getByTestId('idx-prio-1')).toHaveTextContent('25')
    expect(screen.getByTestId('idx-row-1')).toHaveTextContent('standard')
    expect(screen.getByTestId('idx-row-1')).toHaveTextContent('2026-10-01')
    // Category badges on nyaa.
    expect(screen.getByTestId('idx-cats-1')).toHaveTextContent('Movies')
    expect(screen.getByTestId('idx-cats-1')).toHaveTextContent('TV')
    // Status icons: 2 enabled, 1 disabled (nulled), 0 error.
    expect(screen.getAllByTestId('idx-status-enabled')).toHaveLength(2)
    expect(screen.getAllByTestId('idx-status-disabled')).toHaveLength(1)
    expect(screen.queryAllByTestId('idx-status-error')).toHaveLength(0)
  })

  it('renders the A-Z quick-jump with only the present letters', async () => {
    renderHub()
    await screen.findByTestId('idx-az')
    expect(screen.getByTestId('idx-az-B')).toBeEnabled() // btsou
    expect(screen.getByTestId('idx-az-N')).toBeEnabled() // nyaa + nulled
    expect(screen.getByTestId('idx-az-Z')).toBeDisabled()
  })

  it('renders the bottom summary panel: legend + counters', async () => {
    renderHub()
    await screen.findByTestId('idx-summary')
    // Color legend (4 states).
    for (const s of ['Enabled', 'Enabled + Redirected', 'Disabled', 'Error']) {
      expect(screen.getByTestId('idx-summary')).toHaveTextContent(s)
    }
    // Counters: 3 total, 2 enabled, 1 torrent, 2 usenet.
    expect(screen.getByTestId('idx-sum-total')).toHaveTextContent('3')
    expect(screen.getByTestId('idx-sum-enabled')).toHaveTextContent('2')
    expect(screen.getByTestId('idx-sum-torrent')).toHaveTextContent('1')
    expect(screen.getByTestId('idx-sum-usenet')).toHaveTextContent('2')
  })

  it('exposes the page toolbar (Add · Test All · Select · Options · Sort · Filter · Sync)', async () => {
    renderHub()
    await screen.findByTestId('idx-add')
    for (const tid of ['idx-add', 'idx-test-all', 'idx-select', 'idx-options', 'idx-sort', 'idx-filter', 'idx-sync']) {
      expect(screen.getByTestId(tid)).toBeInTheDocument()
    }
  })
})

describe('IndexerHubPage — toolbar behaviors', () => {
  it('Sort by name desc reorders the rows', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-row-1')
    await user.click(screen.getByTestId('idx-sort-btn'))
    await user.click(screen.getByTestId('idx-sort-opt-name-desc'))
    await waitFor(() => {
      // Descending: nulled (N) > nyaa (N, but id tiebreak) > btsou (B). First row should be a "nulled" or "nyaa" (both N, before b).
      const rows = screen.getAllByTestId(/idx-row-/)
      expect(rows[2]).toHaveTextContent('btsou') // btsou last in desc (b is lowest)
    })
  })

  it('Filter by protocol hides other-protocol rows', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-row-1')
    await user.click(screen.getByTestId('idx-filter-btn'))
    await user.click(screen.getByTestId('idx-filter-opt-usenet'))
    await waitFor(() => {
      expect(screen.getByTestId('idx-row-2')).toBeInTheDocument() // btsou (usenet)
      expect(screen.getByTestId('idx-row-3')).toBeInTheDocument() // nulled (usenet)
      expect(screen.queryByTestId('idx-row-1')).not.toBeInTheDocument() // nyaa (torrent) hidden
    })
  })

  it('the A-Z jump filters to the letter and clicking again clears it', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-row-1')
    await user.click(screen.getByTestId('idx-az-N'))
    await waitFor(() => {
      expect(screen.getByTestId('idx-row-1')).toBeInTheDocument() // nyaa
      expect(screen.getByTestId('idx-row-3')).toBeInTheDocument() // nulled
      expect(screen.queryByTestId('idx-row-2')).not.toBeInTheDocument() // btsou hidden
    })
    await user.click(screen.getByTestId('idx-az-N')) // toggle back to all
    await waitFor(() => expect(screen.getByTestId('idx-row-2')).toBeInTheDocument())
  })

  it('Test All Indexers probes every enabled indexer and reports a message', async () => {
    const user = userEvent.setup()
    ;(client.testAllIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([
      tResult(1, true),
      tResult(2, false),
      tResult(3, true),
    ])
    renderHub()
    await screen.findByTestId('idx-test-all')
    await user.click(screen.getByTestId('idx-test-all'))
    await waitFor(() =>
      expect(screen.getByTestId('idx-test-all-msg')).toHaveTextContent('Tested 3'),
    )
    expect(client.testAllIndexers).toHaveBeenCalledTimes(1)
    // The failed probe flips btsou (id 2) to the Error status.
    await waitFor(() => expect(screen.getByTestId('idx-status-error')).toBeInTheDocument())
  })

  it('Sync App Indexers is stubbed and shows a short note', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-sync')
    await user.click(screen.getByTestId('idx-sync'))
    await waitFor(() =>
      expect(screen.getByTestId('idx-sync-note')).toHaveTextContent('per-kind sync'),
    )
  })

  it('Options hides a column via the column-visibility popover', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-proto-1')
    await user.click(screen.getByTestId('idx-options'))
    await user.click(screen.getByTestId('idx-col-protocol'))
    await waitFor(() =>
      expect(screen.queryByTestId('idx-proto-1')).not.toBeInTheDocument(),
    )
  })
})

describe('IndexerHubPage — row actions + add/edit (existing endpoints)', () => {
  it('runs a per-row test and flips a failed row to Error', async () => {
    const user = userEvent.setup()
    ;(client.testIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(tResult(1, false))
    renderHub()
    await screen.findByTestId('idx-test-1')
    await user.click(screen.getByTestId('idx-test-1'))
    await waitFor(() => expect(client.testIndexer).toHaveBeenCalledWith(1))
    await waitFor(() => expect(screen.getByTestId('idx-status-error')).toBeInTheDocument())
  })

  it('toggles enabled state', async () => {
    const user = userEvent.setup()
    ;(client.setIndexerEnabled as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ enabled: false }))
    renderHub()
    await screen.findByTestId('idx-toggle-1')
    await user.click(screen.getByTestId('idx-toggle-1'))
    await waitFor(() => expect(client.setIndexerEnabled).toHaveBeenCalledWith(1, false))
  })

  it('creates an indexer from the Add modal with the new hub fields', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 7, name: 'newidx', kind: 'torznab', base_url: 'https://x' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    await user.selectOptions(screen.getByTestId('idx-kind'), 'torznab')
    await user.type(screen.getByTestId('idx-base'), 'https://x')
    await user.selectOptions(screen.getByTestId('idx-privacy'), 'private')
    const prio = screen.getByTestId('idx-priority')
    await user.clear(prio)
    await user.type(prio, '10')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'newidx',
          kind: 'torznab',
          privacy: 'private',
          priority: 10,
          sync_profile: 'standard',
        }),
      ),
    )
  })

  it('Edit Indexer opens the same modal prefilled and updates with hub fields', async () => {
    const user = userEvent.setup()
    ;(client.updateIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(indexer())
    renderHub()
    await screen.findByTestId('idx-edit-1')
    await user.click(screen.getByTestId('idx-edit-1'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    // Prefilled from the stored indexer.
    expect(screen.getByTestId('idx-name')).toHaveValue('nyaa')
    expect(screen.getByTestId('idx-privacy')).toHaveValue('public')
    expect(screen.getByTestId('idx-priority')).toHaveValue(25)
    const prio = screen.getByTestId('idx-priority')
    await user.clear(prio)
    await user.type(prio, '50')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.updateIndexer).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ name: 'nyaa', kind: 'torrent-rss', privacy: 'public', priority: 50 }),
      ),
    )
  })

  it('saves the per-indexer search toggles through the edit form', async () => {
    const user = userEvent.setup()
    ;(client.updateIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ rss: false }))
    renderHub()
    await screen.findByTestId('idx-edit-1')
    await user.click(screen.getByTestId('idx-edit-1'))
    await waitFor(() => expect(screen.getByTestId('idx-form')).toBeInTheDocument())
    expect(screen.getByTestId('idx-rss')).toBeChecked()
    await user.click(screen.getByTestId('idx-rss'))
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.updateIndexer).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ name: 'nyaa', rss: false, auto_search: true, interactive_search: true }),
      ),
    )
  })

  it('deletes an indexer after confirm', async () => {
    const user = userEvent.setup()
    ;(client.deleteIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    renderHub()
    await screen.findByTestId('idx-delete-1')
    await user.click(screen.getByTestId('idx-delete-1')) // arm
    await user.click(screen.getByTestId('idx-delete-1')) // fire
    await waitFor(() => expect(client.deleteIndexer).toHaveBeenCalledWith(1))
  })

  it('Select Indexers enables a bulk bar that bulk-enables the selected rows', async () => {
    const user = userEvent.setup()
    ;(client.setIndexerEnabled as ReturnType<typeof vi.fn>).mockResolvedValue(indexer())
    renderHub()
    await screen.findByTestId('idx-select')
    await user.click(screen.getByTestId('idx-select'))
    await waitFor(() => expect(screen.getByTestId('idx-select-all')).toBeInTheDocument())
    await user.click(screen.getByTestId('idx-select-all'))
    await waitFor(() => expect(screen.getByTestId('idx-bulkbar')).toBeInTheDocument())
    expect(screen.getByTestId('idx-selected-count')).toHaveTextContent('3')
    await user.click(screen.getByTestId('idx-bulk-enable'))
    await waitFor(() => expect(client.setIndexerEnabled).toHaveBeenCalledTimes(3))
  })
})
