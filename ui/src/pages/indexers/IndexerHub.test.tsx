import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Indexer, IndexerDefinition, IndexerTestResult } from '../../api/types'
import { IndexerHubPage } from './IndexerHub'

// Mock the typed client so the hub talks to in-test fakes.
vi.mock('../../api/client', () => ({
  client: {
    listIndexers: vi.fn(),
    listIndexerDefinitions: vi.fn(),
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

// Prowlarr PR2: fake definition library for the two-stage picker/form flow.
// Covers the kind mapping (torrent -> torrent-rss, usenet -> nzb,
// generic-torznab -> torznab), every contract field type, and an advanced
// field.
const TORRENT_DEF: IndexerDefinition = {
  id: 'test-torrent',
  name: 'Test Torrent',
  protocol: 'torrent',
  privacy: 'public',
  language: '',
  categories: ['Movies', 'TV'],
  urls: ['https://torrent.example.org/api'],
  legacyUrls: [],
  fields: [
    { name: 'username', type: 'textbox', label: 'Username', required: true },
    { name: 'password', type: 'password', label: 'Password' },
    { name: 'note', type: 'info', label: 'Info Title', hint: 'Info body' },
  ],
  presets: [],
}
const USNET_DEF: IndexerDefinition = {
  id: 'test-usenet',
  name: 'Test Usenet',
  protocol: 'usenet',
  privacy: 'private',
  language: '',
  categories: [],
  urls: ['https://usenet.example.org'],
  legacyUrls: [],
  fields: [],
  presets: [],
}
const TORZ_DEF: IndexerDefinition = {
  id: 'generic-torznab',
  name: 'Generic Torznab',
  protocol: 'torrent',
  privacy: 'public',
  language: '',
  categories: [],
  urls: ['https://example.org/torznab'],
  legacyUrls: [],
  fields: [{ name: 'apikey', type: 'password', label: 'API Key' }],
  presets: [],
}
const ALL_TYPES_DEF: IndexerDefinition = {
  id: 'all-types',
  name: 'All Types',
  protocol: 'torrent',
  privacy: 'semi-private',
  language: '',
  categories: ['TV'],
  urls: ['https://types.example.org', 'https://types2.example.org'],
  legacyUrls: [],
  fields: [
    { name: 'textbox', type: 'textbox', label: 'Text Field' },
    { name: 'password', type: 'password', label: 'Secret' },
    { name: 'number', type: 'number', label: 'Count' },
    { name: 'checkbox', type: 'checkbox', label: 'Enable thing' },
    { name: 'select', type: 'select', label: 'Choice', options: { a: 'Option A', b: 'Option B' } },
    { name: 'note', type: 'info', label: 'Info Title', hint: 'Info body' },
    { name: 'adv', type: 'textbox', label: 'Adv Field', advanced: true },
  ],
  presets: [],
}
const DEFS: IndexerDefinition[] = [TORRENT_DEF, USNET_DEF, TORZ_DEF, ALL_TYPES_DEF]

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
  ;(client.listIndexerDefinitions as ReturnType<typeof vi.fn>).mockResolvedValue(DEFS)
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

  it('creates a torrent-definition indexer: kind -> torrent-rss + config_fields + definition_name', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 7, name: 'newidx', kind: 'torrent-rss', base_url: 'https://torrent.example.org/api' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    renderHub()
    // Add now opens the definition picker (stage 1).
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await waitFor(() => expect(screen.getByTestId('idx-picker-modal')).toBeInTheDocument())
    // Pick the torrent definition.
    await user.click(screen.getByTestId('idx-def-test-torrent'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    // The form is prefilled with the definition's base URL + contract fields.
    expect(screen.getByTestId('idx-base')).toHaveValue('https://torrent.example.org/api')
    expect(screen.getByTestId('idx-field-username')).toBeInTheDocument()
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    await user.type(screen.getByTestId('idx-field-username'), 'myuser')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'newidx',
          kind: 'torrent-rss',
          definition_name: 'test-torrent',
          config_fields: expect.objectContaining({ username: 'myuser' }),
          base_url: 'https://torrent.example.org/api',
        }),
      ),
    )
  })

  it('creates a usenet-definition indexer: kind -> nzb + definition_name', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 8, name: 'newnzb', kind: 'nzb', base_url: 'https://usenet.example.org' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await user.click(screen.getByTestId('idx-def-test-usenet'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    await user.type(screen.getByTestId('idx-name'), 'newnzb')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'newnzb', kind: 'nzb', definition_name: 'test-usenet', base_url: 'https://usenet.example.org' }),
      ),
    )
  })

  it('creates a generic-torznab indexer: kind -> torznab (not torrent-rss)', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 9, name: 'newtz', kind: 'torznab', base_url: 'https://example.org/torznab' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await user.click(screen.getByTestId('idx-def-generic-torznab'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    await user.type(screen.getByTestId('idx-name'), 'newtz')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'newtz', kind: 'torznab', definition_name: 'generic-torznab' }),
      ),
    )
  })

  it('blocks create when a required contract field is missing', async () => {
    const user = userEvent.setup()
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ id: 7 }))
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await user.click(screen.getByTestId('idx-def-test-torrent'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    // Leave the required Username blank -> submit is blocked, no API call.
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(screen.getByTestId('idx-form-error')).toHaveTextContent('Username is required'),
    )
    expect(client.createIndexer).not.toHaveBeenCalled()
  })

  it('blocks create when the Base Url is emptied', async () => {
    const user = userEvent.setup()
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ id: 7 }))
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await user.click(screen.getByTestId('idx-def-test-torrent'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    await user.type(screen.getByTestId('idx-field-username'), 'myuser')
    const base = screen.getByTestId('idx-base')
    await user.clear(base)
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() => expect(screen.getByTestId('idx-form-error')).toHaveTextContent('Base Url is required'))
    expect(client.createIndexer).not.toHaveBeenCalled()
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

describe('IndexerHubPage — PR2 definition picker + contract form', () => {
  it('opens the picker on Add and renders every fetched definition with protocol/privacy pills', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await waitFor(() => expect(screen.getByTestId('idx-picker-modal')).toBeInTheDocument())
    // All 4 seeded definitions are present as rows.
    expect(screen.getByTestId('idx-def-test-torrent')).toHaveTextContent('Test Torrent')
    expect(screen.getByTestId('idx-def-test-usenet')).toHaveTextContent('Test Usenet')
    expect(screen.getByTestId('idx-def-generic-torznab')).toHaveTextContent('Generic Torznab')
    expect(screen.getByTestId('idx-def-all-types')).toHaveTextContent('All Types')
    expect(screen.getByTestId('idx-picker-count')).toHaveTextContent('4 of 4 definitions')
  })

  it('search + protocol/privacy/category filters each narrow the picker list', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    // Search narrows to the one matching definition.
    await user.type(screen.getByTestId('idx-picker-search'), 'all types')
    await waitFor(() => expect(screen.getByTestId('idx-picker-count')).toHaveTextContent('1 of 4 definitions'))
    // Clear search, filter by protocol.
    await user.clear(screen.getByTestId('idx-picker-search'))
    await user.selectOptions(screen.getByTestId('idx-picker-protocol'), 'usenet')
    await waitFor(() => expect(screen.getByTestId('idx-picker-count')).toHaveTextContent('1 of 4 definitions'))
    // Reset protocol, filter by privacy.
    await user.selectOptions(screen.getByTestId('idx-picker-protocol'), 'all')
    await user.selectOptions(screen.getByTestId('idx-picker-privacy'), 'private')
    await waitFor(() => expect(screen.getByTestId('idx-picker-count')).toHaveTextContent('1 of 4 definitions'))
    // Reset privacy, filter by category (Movies+TV on torrent, TV on all-types -> 2).
    await user.selectOptions(screen.getByTestId('idx-picker-privacy'), 'all')
    await user.selectOptions(screen.getByTestId('idx-picker-category'), 'TV')
    await waitFor(() => expect(screen.getByTestId('idx-picker-count')).toHaveTextContent('2 of 4 definitions'))
  })

  it('picking a definition renders every contract field type; advanced fields stay hidden until toggled', async () => {
    const user = userEvent.setup()
    renderHub()
    await screen.findByTestId('idx-add')
    await user.click(screen.getByTestId('idx-add'))
    await user.click(screen.getByTestId('idx-def-all-types'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    // Picker closed, form open.
    expect(screen.queryByTestId('idx-picker-modal')).not.toBeInTheDocument()
    // Base Url is a SELECT (the def has two candidate URLs).
    const base = screen.getByTestId('idx-base')
    expect(base).toHaveValue('https://types.example.org')
    expect((base as HTMLSelectElement).tagName).toBe('SELECT')
    // Every contract field type renders as the right input.
    expect((screen.getByTestId('idx-field-textbox') as HTMLInputElement).type).toBe('text')
    expect((screen.getByTestId('idx-field-password') as HTMLInputElement).type).toBe('password')
    expect((screen.getByTestId('idx-field-number') as HTMLInputElement).type).toBe('number')
    expect(screen.getByTestId('idx-field-checkbox')).toBeInTheDocument()
    expect((screen.getByTestId('idx-field-select') as HTMLSelectElement).tagName).toBe('SELECT')
    // Info renders as static text, not an input.
    expect(screen.getByTestId('idx-field-note')).toHaveTextContent('Info Title')
    expect(screen.getByTestId('idx-field-note')).toHaveTextContent('Info body')
    // Advanced field is hidden until the toggle.
    expect(screen.queryByTestId('idx-field-adv')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('idx-show-advanced'))
    await waitFor(() => expect(screen.getByTestId('idx-field-adv')).toBeInTheDocument())
  })

  it('editing a PR2 indexer prefills the contract and keeps a blank password (masked-edit round-trip)', async () => {
    const user = userEvent.setup()
    const def = TORRENT_DEF
    ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([
      indexer({
        id: 11,
        name: 'masked',
        kind: 'torrent-rss',
        base_url: 'https://torrent.example.org/api',
        definition_name: def.id,
        config_fields: { username: 'stored-user', password: '' },
      }),
    ])
    ;(client.updateIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(
      indexer({ id: 11, definition_name: def.id, config_fields: { username: 'stored-user', password: '' } }),
    )
    renderHub()
    await screen.findByTestId('idx-edit-11')
    await user.click(screen.getByTestId('idx-edit-11'))
    await waitFor(() => expect(screen.getByTestId('idx-form-modal')).toBeInTheDocument())
    // Contract field prefilled with the stored value.
    expect(screen.getByTestId('idx-field-username')).toHaveValue('stored-user')
    // Password arrives masked '' -> placeholder dots + '(blank to keep)' hint.
    const pw = screen.getByTestId('idx-field-password') as HTMLInputElement
    expect(pw.type).toBe('password')
    expect(pw.value).toBe('')
    expect(pw).toHaveAttribute('placeholder', '••••••••')
    expect(screen.getByTestId('idx-form')).toHaveTextContent('(blank to keep)')
    // Save keeps the blank password (empty = keep the stored secret).
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.updateIndexer).toHaveBeenCalledWith(
        11,
        expect.objectContaining({
          name: 'masked',
          kind: 'torrent-rss',
          definition_name: def.id,
          config_fields: expect.objectContaining({ username: 'stored-user', password: '' }),
        }),
      ),
    )
  })
})
