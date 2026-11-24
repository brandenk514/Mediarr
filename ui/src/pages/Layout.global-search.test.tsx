import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { TopBar } from './Layout'

// Global search bar behavior (issue #117): the input holds a draft query and
// fires NOTHING on keystrokes — no per-kind endpoint is called while typing.
// Only form submission (Enter in the input, or the dedicated submit button)
// navigates to the results route carrying the query in the URL (?q=…).
vi.mock('../api/client', () => ({
  client: {
    // The bar itself never calls the API; these exist only so the module
    // graph resolves and so we can assert they are NOT called on typing.
    searchMovies: vi.fn(),
    searchTv: vi.fn(),
    searchArtists: vi.fn(),
    lookupBooks: vi.fn(),
  },
}))

// Probe renders the current location so navigation is observable.
function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location-probe">{location.pathname}{location.search}</div>
}

function renderBar(initial = '/movies') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <TopBar version="dev" username="admin" onLogout={vi.fn()} />
      <Routes>
        <Route path="/movies" element={<LocationProbe />} />
        <Route path="/search/all" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

function probeText(): string {
  return screen.getByTestId('location-probe').textContent ?? ''
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('TopBar global search (issue #117)', () => {
  it('renders the input and a (disabled) submit button', () => {
    renderBar()
    const input = screen.getByTestId('global-search')
    expect(input).toBeInTheDocument()
    expect(input).toHaveAttribute('aria-label', 'Global search')
    const submit = screen.getByTestId('global-search-submit')
    expect(submit).toBeInTheDocument()
    expect(submit).toBeDisabled()
  })

  it('typing alone does not navigate and fires no search requests', async () => {
    const user = userEvent.setup()
    renderBar()
    await user.type(screen.getByTestId('global-search'), 'du')
    expect(probeText()).toBe('/movies')
    // No per-kind endpoint may be called on keystrokes.
    expect(client.searchMovies).not.toHaveBeenCalled()
    expect(client.searchTv).not.toHaveBeenCalled()
    expect(client.searchArtists).not.toHaveBeenCalled()
    expect(client.lookupBooks).not.toHaveBeenCalled()
  })

  it('pressing Enter navigates to the results route with the query in the URL', async () => {
    const user = userEvent.setup()
    renderBar()
    await user.type(screen.getByTestId('global-search'), '  dune  {enter}')
    expect(probeText()).toBe('/search/all?q=dune')
  })

  it('the dedicated submit button navigates too', async () => {
    const user = userEvent.setup()
    renderBar()
    const input = screen.getByTestId('global-search')
    await user.type(input, 'radiohead')
    expect(screen.getByTestId('global-search-submit')).toBeEnabled()
    await user.click(screen.getByTestId('global-search-submit'))
    expect(probeText()).toBe('/search/all?q=radiohead')
  })

  it('encodes special characters in the query', async () => {
    const user = userEvent.setup()
    renderBar()
    await user.type(screen.getByTestId('global-search'), 'a b + c{enter}')
    expect(probeText()).toBe('/search/all?q=a%20b%20%2B%20c')
  })

  it('does not navigate on an empty or whitespace-only query', async () => {
    const user = userEvent.setup()
    renderBar()
    // Blank draft: the button is disabled and Enter is a no-op.
    expect(screen.getByTestId('global-search-submit')).toBeDisabled()
    await user.type(screen.getByTestId('global-search'), '   {enter}')
    expect(probeText()).toBe('/movies')
  })
})
