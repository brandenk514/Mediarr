import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { ImportListsSection } from './ImportListsSection'

// Mock the typed client; the list configs ride the per-section document
// endpoint and the run action rides the P5 /import-lists/import endpoint.
vi.mock('../../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    constructor(status: number) {
      super('boom')
      this.status = status
    }
  },
  client: {
    getSettingsSection: vi.fn(),
    putSettingsSection: vi.fn(),
    runImportListImport: vi.fn(),
  },
}))

const FRESH = {
  lists: [
    {
      id: 1,
      name: 'my shows',
      kind: 'trakt',
      url: 'https://trakt.tv/lists/1',
      enabled: true,
      api_key: '',
      api_key_set: false,
    },
  ],
}

function renderSection() {
  return render(
    <MemoryRouter>
      <ImportListsSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue(FRESH)
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_s: string, doc: unknown) => doc as never,
  )
  ;(client.runImportListImport as ReturnType<typeof vi.fn>).mockImplementation(async (req: {
    dry_run: boolean
  }) => ({
    dry_run: req.dry_run,
    total: 2,
    items: [
      { external_id: '1', title: 'Show A', kind: 'tv', status: 'imported', message: '' },
      { external_id: '2', title: 'Show B', kind: 'tv', status: 'exists', message: 'already in library' },
    ],
  }))
})

describe('ImportListsSection', () => {
  it('renders the seeded list', async () => {
    renderSection()
    expect(await screen.findByTestId('il-item-1')).toBeInTheDocument()
    expect(screen.getByTestId('il-kind-1')).toHaveTextContent('trakt')
    expect(screen.getByTestId('il-url-1')).toHaveTextContent('https://trakt.tv/lists/1')
  })

  it('runs a dry-run import through the P5 import endpoint', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('il-item-1')
    // dry-run is on by default; the run button label says so.
    expect(screen.getByTestId('il-run-1')).toHaveTextContent('Dry-run')
    await user.click(screen.getByTestId('il-run-1'))
    await vi.waitFor(() =>
      expect(client.runImportListImport).toHaveBeenCalledWith({ id: 1, dry_run: true }),
    )
    expect(await screen.findByTestId('il-run-summary')).toHaveTextContent('dry run: 2 items')
    expect(screen.getByTestId('il-run-item-imported')).toHaveTextContent('Show A')
    expect(screen.getByTestId('il-run-item-exists')).toHaveTextContent('Show B [tv] exists — already in library')
  })

  it('adds a Sonarr-bridge list and saves the doc', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('il-item-1')
    await user.type(screen.getByTestId('il-new-name'), 'sonarr-1')
    await user.selectOptions(screen.getByTestId('il-new-kind'), 'sonarr')
    await user.type(screen.getByTestId('il-new-url'), 'http://sonarr.local:8989')
    await user.click(screen.getByTestId('il-add'))
    expect(await screen.findByTestId('il-item-2')).toBeInTheDocument()
    await user.click(screen.getByTestId('il-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('import-lists', {
        lists: [
          {
            id: 1,
            name: 'my shows',
            kind: 'trakt',
            url: 'https://trakt.tv/lists/1',
            enabled: true,
            api_key: '',
            api_key_set: false,
          },
          {
            id: 2,
            name: 'sonarr-1',
            kind: 'sonarr',
            url: 'http://sonarr.local:8989',
            enabled: true,
            api_key: '',
            api_key_set: false,
          },
        ],
      }),
    )
  })
})
