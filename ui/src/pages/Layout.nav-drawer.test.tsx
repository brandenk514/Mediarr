import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { AuthProvider } from '../auth/AuthContext'
import { Layout } from './Layout'

// The top bar fetches /version raw; keep it off the network in tests.
vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))

vi.mock('../api/client', () => ({
  client: {
    // The drawer's Sidebar polls the health badge endpoint.
    getSystemHealth: vi.fn().mockResolvedValue({ checks: [], ok: 1, warning: 0, error: 0 }),
  },
}))

// The responsive shell's breakpoint (issue #123).
function stubMobile(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    })),
  )
}

function renderLayout(initial = '/library') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <AuthProvider>
        <Layout>
          <div data-testid="page-content">content</div>
        </Layout>
      </AuthProvider>
    </MemoryRouter>,
  )
}

// Like renderLayout, but the page content observes the current location so
// nav-triggered routing is observable.
function renderLayoutWithLocationProbe(initial = '/library') {
  let observed = ''
  function LocationProbe() {
    const location = useLocation()
    observed = `${location.pathname}${location.search}`
    return null
  }
  render(
    <MemoryRouter initialEntries={[initial]}>
      <AuthProvider>
        <Layout>
          <LocationProbe />
        </Layout>
      </AuthProvider>
    </MemoryRouter>,
  )
  return {
    get observedLocation() {
      return observed
    },
  }
}

const RAIL_IDS = [
  'dashboard',
  'discover',
  'library',
  'calendar',
  'activity',
  'indexers',
  'settings',
  'system',
]

beforeEach(() => vi.clearAllMocks())
afterEach(() => vi.unstubAllGlobals())

describe('mobile nav drawer (issue #123)', () => {
  it('renders no menu toggle at desktop widths (rail stays visible)', () => {
    stubMobile(false)
    renderLayout()
    expect(screen.queryByTestId('nav-toggle')).not.toBeInTheDocument()
    // The fixed rail is the desktop navigation.
    expect(screen.getByTestId('rail-dashboard')).toBeInTheDocument()
  })

  it('renders the menu toggle at mobile widths and opens the drawer with the same rail', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    renderLayout()
    const toggle = screen.getByTestId('nav-toggle')
    expect(toggle).toHaveAttribute('aria-label', 'Menu')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    // The fixed rail is hidden from the mobile shell.
    expect(screen.queryByTestId('rail-dashboard')).not.toBeInTheDocument()

    await user.click(toggle)

    const drawer = screen.getByTestId('nav-drawer')
    expect(drawer).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    // The SAME Sidebar component: all eight sections keep their rail-* testids.
    for (const id of RAIL_IDS) {
      expect(drawer).toContainElement(screen.getByTestId(`rail-${id}`))
    }
    // The Library kind rows are revealed while on /library (sub-nav intact).
    expect(screen.getByTestId('rail-sub-tv')).toBeInTheDocument()
  })

  it('moves focus into the drawer on open and back to the toggle on close', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    renderLayout()
    const toggle = screen.getByTestId('nav-toggle')
    await user.click(toggle)
    const drawer = screen.getByTestId('nav-drawer')
    expect(drawer.contains(document.activeElement as HTMLElement)).toBe(true)

    await user.keyboard('{Escape}')
    expect(screen.queryByTestId('nav-drawer')).not.toBeInTheDocument()
    expect(toggle).toHaveFocus()
  })

  it('renders a close button that closes the drawer (and restores focus)', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    renderLayout()
    const toggle = screen.getByTestId('nav-toggle')
    await user.click(toggle)

    const close = screen.getByTestId('nav-drawer-close')
    expect(close).toHaveAttribute('aria-label', 'Close menu')
    expect(screen.getByTestId('nav-drawer')).toContainElement(close)

    await user.click(close)
    expect(screen.queryByTestId('nav-drawer')).not.toBeInTheDocument()
    expect(toggle).toHaveFocus()
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  it('closes on Escape', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    renderLayout()
    await user.click(screen.getByTestId('nav-toggle'))
    expect(screen.getByTestId('nav-drawer')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(screen.queryByTestId('nav-drawer')).not.toBeInTheDocument()
    expect(screen.getByTestId('nav-toggle')).toHaveAttribute('aria-expanded', 'false')
  })

  it('closes on a backdrop click, but not on a click inside the drawer', async () => {
    stubMobile(true)
    renderLayout()
    await act(async () => {
      screen.getByTestId('nav-toggle').click()
    })
    const overlay = screen.getByTestId('nav-drawer-overlay')
    const drawer = screen.getByTestId('nav-drawer')

    // A click landing inside the drawer must NOT close it.
    act(() => {
      drawer.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(screen.getByTestId('nav-drawer')).toBeInTheDocument()

    // A click on the bare backdrop closes it.
    act(() => {
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(screen.queryByTestId('nav-drawer')).not.toBeInTheDocument()
  })

  it('closes when a nav item is selected (and navigates)', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    const probe = renderLayoutWithLocationProbe('/library')
    await user.click(screen.getByTestId('nav-toggle'))
    expect(screen.getByTestId('nav-drawer')).toBeInTheDocument()
    await user.click(screen.getByTestId('rail-dashboard'))
    expect(screen.queryByTestId('nav-drawer')).not.toBeInTheDocument()
    // Read the getter AFTER the navigation: it reflects the settled route.
    expect(probe.observedLocation).toBe('/dashboard')
  })

  it('locks body scroll while open and restores it on close', async () => {
    stubMobile(true)
    const user = userEvent.setup()
    renderLayout()
    expect(document.body.style.overflow).toBe('')
    await user.click(screen.getByTestId('nav-toggle'))
    expect(document.body.style.overflow).toBe('hidden')
    await user.keyboard('{Escape}')
    expect(document.body.style.overflow).toBe('')
  })

  it('health badge still renders on the System item inside the drawer', async () => {
    stubMobile(true)
    vi.mocked(client.getSystemHealth).mockResolvedValue({
      checks: [],
      ok: 1,
      warning: 2,
      error: 1,
    })
    renderLayout()
    await act(async () => {
      screen.getByTestId('nav-toggle').click()
    })
    const badge = await screen.findByTestId('rail-system-badge')
    expect(badge).toHaveTextContent('3')
  })
})
