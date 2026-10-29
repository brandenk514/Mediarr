import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { IndexersSection } from './IndexersSection'

// IndexersSection is a hub pointer (Prowlarr PR1): the list moved to the
// first-class /indexers hub. The settings section keeps the `idx-card` testid
// (the SettingsShell contract) and links to the hub.
function renderSection() {
  return render(
    <MemoryRouter>
      <IndexersSection />
    </MemoryRouter>,
  )
}

describe('IndexersSection (hub pointer)', () => {
  it('keeps the idx-card and points to the /indexers hub', () => {
    renderSection()
    expect(screen.getByTestId('idx-card')).toBeInTheDocument()
    const link = screen.getByTestId('idx-hub-link')
    expect(link).toHaveTextContent('Open Indexers hub')
    expect(link).toHaveAttribute('href', '/indexers')
  })

  it('no longer renders the inline add/table surface', () => {
    renderSection()
    // The CRUD surface now lives in the hub; the settings section is a pointer.
    expect(screen.queryByTestId('idx-add')).not.toBeInTheDocument()
    expect(screen.queryByTestId('idx-table')).not.toBeInTheDocument()
  })
})
