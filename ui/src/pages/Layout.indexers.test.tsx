import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { Rail } from './Layout'

// The rail renders a System badge from the health endpoint; keep it off the
// network in this focused rail test.
vi.mock('../api/client', () => ({
  client: {
    getSystemHealth: vi.fn(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({ checks: [], ok: 1, warning: 0, error: 0 })
})

describe('Rail — Prowlarr PR1 Indexers shared section', () => {
  it('renders the Indexers section (home /indexers) with a Stats sub-item', async () => {
    render(
      <MemoryRouter initialEntries={['/indexers']}>
        <Rail />
      </MemoryRouter>,
    )
    const section = await screen.findByTestId('rail-indexers')
    expect(section).toBeInTheDocument()
    // The section header is a link to the hub home.
    expect(section).toHaveAttribute('href', '/indexers')
    expect(section).toHaveTextContent('Indexers')
    // The Stats stub sub-item is reachable (in-scope at /indexers).
    expect(screen.getByTestId('rail-sub-indexersstats')).toHaveAttribute('href', '/indexers/stats')
  })
})
