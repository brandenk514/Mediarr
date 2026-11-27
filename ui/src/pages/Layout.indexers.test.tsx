import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { Sidebar } from './Layout'

// Issue #148: Indexers is no longer a sidebar section — it is parked as a
// sub-link under Settings (interim reachability until #154 folds it into a
// Settings tab), keeping its Prowlarr sub-pages as nested sub-links.
describe('Sidebar — Indexers under Settings (issue #148)', () => {
  it('reveals the Indexers sub-link and its Search/History/Stats sub-links while on an Indexers route', () => {
    render(
      <MemoryRouter initialEntries={['/indexers']}>
        <Sidebar />
      </MemoryRouter>,
    )
    // The whole indexer area keeps highlighting the Settings section…
    const settings = screen.getByTestId('rail-settings')
    expect(settings).toHaveAttribute('aria-current', 'page')
    // …and the Indexers sub-link is revealed (it is the active group)…
    const indexers = screen.getByTestId('rail-sub-indexers')
    expect(indexers).toBeInTheDocument()
    expect(indexers).toHaveAttribute('aria-current', 'page')
    // …with its Prowlarr sub-pages nested beneath it.
    expect(screen.getByTestId('rail-sub-search')).toBeInTheDocument()
    expect(screen.getByTestId('rail-sub-history')).toBeInTheDocument()
    expect(screen.getByTestId('rail-sub-indexersstats')).toBeInTheDocument()
  })

  it('shows the Indexers sub-link under Settings on the Settings route (nested sub-links hidden)', () => {
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <Sidebar />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('rail-sub-indexers')).toBeInTheDocument()
    expect(screen.queryByTestId('rail-sub-search')).not.toBeInTheDocument()
    expect(screen.queryByTestId('rail-sub-history')).not.toBeInTheDocument()
    expect(screen.queryByTestId('rail-sub-indexersstats')).not.toBeInTheDocument()
  })
})
