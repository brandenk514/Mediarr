import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { IndexerStatsPage } from './IndexerStats'

// The Stats sub-page is a PR1 stub (the real health rollup lands in PR3): it
// exists and is reachable at /indexers/stats, and points back to the hub.
describe('IndexerStatsPage (PR1 stub)', () => {
  it('renders the stub and links back to the hub', () => {
    render(
      <MemoryRouter initialEntries={['/indexers/stats']}>
        <IndexerStatsPage />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('indexers-stats-title')).toHaveTextContent('Indexer Stats')
    const back = screen.getByTestId('indexers-stats-back')
    expect(back).toHaveAttribute('href', '/indexers')
  })
})
