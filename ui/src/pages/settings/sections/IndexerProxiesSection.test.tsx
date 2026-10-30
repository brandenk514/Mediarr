import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../../api/client'
import type { IndexerProxy } from '../../../api/types'
import { IndexerProxiesSection } from './IndexerProxiesSection'

// Mock the typed client so the section talks to in-test fakes.
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
    listIndexerProxies: vi.fn(),
    createIndexerProxy: vi.fn(),
    updateIndexerProxy: vi.fn(),
    deleteIndexerProxy: vi.fn(),
    testIndexerProxy: vi.fn(),
    getSettingsSection: vi.fn(),
  },
}))

function proxy(partial: Partial<IndexerProxy> = {}): IndexerProxy {
  return {
    id: 1,
    name: 'flaresolverr',
    url: 'http://flaresolverr:8191',
    username: '',
    password: '',
    password_set: true,
    tags: ['private'],
    created_at: '2026-10-29T00:00:00Z',
    ...partial,
  }
}

function renderSection() {
  return render(
    <MemoryRouter>
      <IndexerProxiesSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([])
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue({
    tags: [
      { id: 1, label: 'private', description: '' },
      { id: 2, label: 'usenet', description: '' },
    ],
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IndexerProxiesSection', () => {
  it('shows the empty state with an add button on a fresh install', async () => {
    renderSection()
    expect(await screen.findByTestId('prox-empty')).toBeInTheDocument()
    expect(screen.getByTestId('prox-empty-add')).toBeInTheDocument()
  })

  it('lists proxies with the masked password and the routing tags', async () => {
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([
      proxy(),
      proxy({ id: 2, name: 'fs2', tags: [], password_set: false }),
    ])
    renderSection()
    await screen.findByTestId('prox-table')
    expect(screen.getByTestId('prox-row-1')).toHaveTextContent('flaresolverr')
    expect(screen.getByTestId('prox-password-1')).toHaveTextContent('set')
    expect(screen.getByTestId('prox-password-2')).toHaveTextContent('—')
    expect(screen.getByTestId('prox-tags-1')).toHaveTextContent('private')
    expect(screen.getByTestId('prox-tags-2')).toHaveTextContent('—')
  })

  it('creates a proxy with the selected routing tags', async () => {
    const user = userEvent.setup()
    ;(client.createIndexerProxy as ReturnType<typeof vi.fn>).mockResolvedValue(proxy({ id: 9 }))
    renderSection()
    await screen.findByTestId('prox-empty')
    await user.click(screen.getByTestId('prox-add'))
    await user.type(screen.getByTestId('prox-f-name'), 'flaresolverr')
    await user.type(screen.getByTestId('prox-f-url'), 'http://flaresolverr:8191')
    // Select the "private" routing tag from the tags document.
    fireEvent.click(screen.getByTestId('prox-tag-private'))
    await user.click(screen.getByTestId('prox-save'))
    await waitFor(() => {
      expect(client.createIndexerProxy).toHaveBeenCalledWith({
        name: 'flaresolverr',
        url: 'http://flaresolverr:8191',
        username: '',
        password: '',
        tags: ['private'],
      })
    })
  })

  it('edits a proxy with a blank (keep-stored) password', async () => {
    const user = userEvent.setup()
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([proxy()])
    ;(client.updateIndexerProxy as ReturnType<typeof vi.fn>).mockResolvedValue(proxy())
    renderSection()
    await screen.findByTestId('prox-row-1')
    await user.click(screen.getByTestId('prox-edit-1'))
    // The form re-opens seeded from the row; the password field is blank on
    // edit (a blank save keeps the stored value).
    expect(screen.getByTestId('prox-f-password')).toHaveValue('')
    expect(screen.getByTestId('prox-f-url')).toHaveValue('http://flaresolverr:8191')
    fireEvent.change(screen.getByTestId('prox-f-name'), { target: { value: 'renamed' } })
    await user.click(screen.getByTestId('prox-save'))
    await waitFor(() => {
      expect(client.updateIndexerProxy).toHaveBeenCalledWith(1, {
        name: 'renamed',
        url: 'http://flaresolverr:8191',
        username: '',
        password: '',
        tags: ['private'],
      })
    })
  })

  it('runs the per-proxy health test and shows the outcome', async () => {
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([proxy()])
    ;(client.testIndexerProxy as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      version: '3.0.9',
    })
    renderSection()
    await screen.findByTestId('prox-row-1')
    fireEvent.click(screen.getByTestId('prox-test-1'))
    expect(await screen.findByTestId('prox-test-result-1')).toHaveTextContent('ok: 3.0.9')
  })

  it('deletes a proxy', async () => {
    const user = userEvent.setup()
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([proxy()])
    ;(client.deleteIndexerProxy as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    renderSection()
    await screen.findByTestId('prox-row-1')
    await user.click(screen.getByTestId('prox-del-1'))
    await waitFor(() => expect(client.deleteIndexerProxy).toHaveBeenCalledWith(1))
  })

  it('shows a section error state when the proxies load fails', async () => {
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'boom'),
    )
    renderSection()
    expect(await screen.findByTestId('prox-error')).toBeInTheDocument()
  })

  it('survives a tags-document failure (the picker degrades, the list does not)', async () => {
    const user = userEvent.setup()
    ;(client.listIndexerProxies as ReturnType<typeof vi.fn>).mockResolvedValue([proxy()])
    ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'tags unavailable'),
    )
    renderSection()
    // The row rendered: the tags failure did not take the list down.
    await screen.findByTestId('prox-row-1')
    expect(screen.queryByTestId('prox-error')).not.toBeInTheDocument()
    // The form opens with a picker that offers no labels.
    await user.click(screen.getByTestId('prox-add'))
    expect(screen.getByTestId('prox-f-tags')).toHaveTextContent('No tags defined yet')
  })
})
