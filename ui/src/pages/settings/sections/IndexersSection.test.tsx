import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import type { Indexer } from '../../../api/types'
import { IndexersSection } from './IndexersSection'

// Mock the typed client so the section talks to in-test fakes.
vi.mock('../../../api/client', () => ({
  client: {
    listIndexers: vi.fn(),
    testIndexer: vi.fn(),
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
    ...over,
  }
}

function renderSection() {
  return render(
    <MemoryRouter>
      <IndexersSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([indexer()])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IndexersSection', () => {
  it('loads the indexer list and shows the seeded row', async () => {
    renderSection()
    // Indexers table shows the seeded row.
    expect(await screen.findByTestId('idx-row-1')).toHaveTextContent('nyaa')
    // Masked key state: nyaa has no key → "—".
    expect(screen.getByTestId('idx-row-1')).toHaveTextContent('torrent-rss')
  })

  it('creates an indexer from the add form', async () => {
    const user = userEvent.setup()
    const created = indexer({ id: 7, name: 'newidx', kind: 'torznab', base_url: 'https://x' })
    ;(client.createIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(created)
    ;(client.listIndexers as ReturnType<typeof vi.fn>).mockResolvedValue([indexer(), created])
    renderSection()
    await waitFor(() => expect(screen.getByTestId('idx-add')).toBeVisible())

    await user.click(screen.getByTestId('idx-add'))
    await waitFor(() => expect(screen.getByTestId('idx-name')).toBeVisible())
    await user.type(screen.getByTestId('idx-name'), 'newidx')
    await user.selectOptions(screen.getByTestId('idx-kind'), 'torznab')
    await user.type(screen.getByTestId('idx-base'), 'https://x')
    await user.click(screen.getByTestId('idx-save'))
    await waitFor(() =>
      expect(client.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'newidx', kind: 'torznab' }),
      ),
    )
  })

  it('toggles indexer enabled state', async () => {
    const user = userEvent.setup()
    ;(client.setIndexerEnabled as ReturnType<typeof vi.fn>).mockResolvedValue(indexer({ enabled: false }))
    renderSection()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    await user.click(screen.getByTestId('idx-toggle-1'))
    await waitFor(() =>
      expect(client.setIndexerEnabled).toHaveBeenCalledWith(1, false),
    )
  })

  it('runs an indexer test and shows the result pill', async () => {
    const user = userEvent.setup()
    ;(client.testIndexer as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true })
    renderSection()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    await user.click(screen.getByTestId('idx-test-1'))
    await waitFor(() =>
      expect(client.testIndexer).toHaveBeenCalledWith(1),
    )
    await waitFor(() =>
      expect(screen.getByTestId('idx-row-1')).toHaveTextContent('ok'),
    )
  })

  it('deletes an indexer after confirm', async () => {
    const user = userEvent.setup()
    ;(client.deleteIndexer as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    renderSection()
    await waitFor(() => expect(screen.getByTestId('idx-row-1')).toBeVisible())
    // First click arms the delete ("Confirm?"), second fires it.
    await user.click(screen.getByTestId('idx-delete-1'))
    await user.click(screen.getByTestId('idx-delete-1'))
    await waitFor(() =>
      expect(client.deleteIndexer).toHaveBeenCalledWith(1),
    )
  })
})
