import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { Sidebar } from './Layout'

// The sidebar renders a System badge from the health endpoint; keep it off
// the network in this focused rail test.
vi.mock('../api/client', () => ({
  client: {
    getSystemHealth: vi.fn(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({ checks: [], ok: 1, warning: 0, error: 0 })
})

describe('Sidebar — Indexers section (unified shell, issue #116)', () => {
  it('renders the Indexers section (home /indexers) with Search / History / Stats sub-links', async () => {
    render(
      <MemoryRouter initialEntries={['/indexers']}>
        <Sidebar />
      </MemoryRouter>,
    )
    const section = await screen.findByTestId('rail-indexers')
    expect(section).toBeInTheDocument()
    expect(section).toHaveTextContent('Indexers')
    // The sub-links are revealed while the route is in scope (/indexers).
    expect(screen.getByTestId('rail-sub-search')).toBeInTheDocument()
    expect(screen.getByTestId('rail-sub-history')).toBeInTheDocument()
    expect(screen.getByTestId('rail-sub-indexersstats')).toBeInTheDocument()
  })
})
