import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { AuthProvider } from '../auth/AuthContext'
import { Layout, versionPillLabel } from './Layout'

// App shell (issue #148): the Seerr shape — no top bar. The sidebar carries
// the brand (top), the nav (middle), and the version pill (bottom, fed by the
// existing /version endpoint). The content header carries the pill search
// and the circular avatar menu (admin identity + sign-out, real menu
// semantics).
vi.mock('../api/client', () => ({
  client: {
    // Auth validation + the sidebar's health badge.
    me: vi.fn().mockResolvedValue({ username: 'admin' }),
    login: vi.fn(),
    logout: vi.fn(),
    getSystemHealth: vi.fn().mockResolvedValue({ checks: [], ok: 1, warning: 0, error: 0 }),
  },
}))

vi.mock('../api/store', () => ({
  // A token in memory: AuthProvider validates it (client.me -> admin).
  getToken: vi.fn().mockReturnValue('tok'),
}))

// /version is fetched raw by the shell; point it where each test wants.
function stubVersion(version: string) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => (version ? { version } : {}),
    }),
  )
}

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

function renderShell() {
  return render(
    <MemoryRouter initialEntries={['/movies']}>
      <AuthProvider>
        <Layout>
          <div data-testid="page-content">content</div>
        </Layout>
      </AuthProvider>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})
afterEach(() => {
  vi.unstubAllGlobals()
})

// ---- sidebar: brand + version pill (no top bar) ----

describe('sidebar shell (issue #148)', () => {
  it('puts the brand in the sidebar, and no .topbar element remains', async () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    const brand = await screen.findByTestId('brand')
    // The brand lives inside the rail (sidebar), not a header.
    expect(brand.closest('.rail')).toBeTruthy()
    expect(document.querySelector('.topbar')).toBeNull()
  })

  it('shows the version pill in the sidebar when /version returns a version', async () => {
    stubMobile(false)
    stubVersion('0.4.0')
    renderShell()
    const pill = await screen.findByTestId('version-pill')
    // The pill sits in the sidebar (the rail), fed by the /version fetch.
    expect(pill.closest('.rail')).toBeTruthy()
    expect(pill).toHaveTextContent('Stable')
    expect(pill).toHaveTextContent('0.4.0')
  })

  it('hides the version pill gracefully when /version is empty', async () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    await screen.findByTestId('brand')
    expect(screen.queryByTestId('version-pill')).not.toBeInTheDocument()
  })

  it('renders the nine sections in the sidebar rail', async () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    for (const id of [
      'discover',
      'movie',
      'tv',
      'music',
      'book',
      'activity',
      'calendar',
      'settings',
      'system',
    ]) {
      expect(await screen.findByTestId(`rail-${id}`)).toBeInTheDocument()
    }
  })
})

// ---- content header: pill search + avatar menu ----

describe('content header (issue #148)', () => {
  it('holds the pill global search and the avatar menu button', () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    // The search keeps its #117 testids (moved from the retired top bar).
    expect(screen.getByTestId('global-search-form')).toBeInTheDocument()
    expect(screen.getByTestId('global-search')).toBeInTheDocument()
    expect(screen.getByTestId('global-search-submit')).toBeInTheDocument()
    expect(screen.getByTestId('user-menu')).toBeInTheDocument()
    // The header is not the retired .topbar.
    const form = screen.getByTestId('global-search-form')
    expect(form.closest('.topbar')).toBeNull()
  })

  it('renders no menu toggle at desktop widths', () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    expect(screen.queryByTestId('nav-toggle')).not.toBeInTheDocument()
  })
})

// ---- user menu (circular avatar button with real menu semantics) ----

describe('user menu (avatar button, issue #148)', () => {
  it('opens the menu: admin identity + sign-out; trigger carries aria-haspopup/aria-expanded', async () => {
    stubMobile(false)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    const trigger = await screen.findByTestId('user-menu')
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')

    await user.click(trigger)

    const menu = await screen.findByRole('menu')
    // The admin identity is shown in the menu…
    expect(menu).toHaveTextContent('admin')
    // …and the sign-out action (the 'logout' testid moved here from the
    // retired top bar).
    expect(screen.getByTestId('logout')).toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
  })

  it('closes on Escape and returns focus to the trigger', async () => {
    stubMobile(false)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    const trigger = await screen.findByTestId('user-menu')
    await user.click(trigger)
    await screen.findByRole('menu')

    await user.keyboard('{Escape}')

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  it('moves focus into the menu on open and keeps it on the items under arrow keys', async () => {
    stubMobile(false)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    const trigger = await screen.findByTestId('user-menu')
    await user.click(trigger)

    const items = await screen.findAllByRole('menuitem')
    expect(items.length).toBeGreaterThan(0)
    // Focus enters the menu (first item) on open…
    expect(items[0]).toHaveFocus()
    // …and arrow keys navigate within the menu (wrapping for a single item).
    await user.keyboard('{ArrowDown}')
    expect(items[items.length - 1]).toHaveFocus()
    await user.keyboard('{ArrowUp}')
    expect(items[0]).toHaveFocus()
    // The menu stays open throughout.
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
  })

  it('signing out calls logout and closes the menu', async () => {
    stubMobile(false)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    const trigger = await screen.findByTestId('user-menu')
    await user.click(trigger)
    await screen.findByTestId('logout')

    await user.click(screen.getByTestId('logout'))

    expect(client.logout).toHaveBeenCalled()
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('closes on an outside click (focus stays out of the menu)', async () => {
    stubMobile(false)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    const trigger = await screen.findByTestId('user-menu')
    await user.click(trigger)
    await screen.findByRole('menu')

    await user.click(screen.getByTestId('page-content'))

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })
})

// ---- icons: no emoji / unicode-glyph characters in nav markup ----

// The legacy nav icon set (▦ ✨ 📚 ▶ 🎬 ♫ 📖 📅 ⚡ 🧲 ⚙ 🖥, plus the
// chrome glyphs ☰ ⌕ ♡ ✕) is retired: icons are SVG line components now.
// The range below covers arrows/geometric/dingbat/symbol blocks and the
// emoji planes — nothing in the new shell markup may carry such a char.
const GLYPH_OR_EMOJI = /[\u2190-\u2BFF\u{1F000}-\u{1FAFF}]/u

describe('nav markup (issue #148)', () => {
  it('uses no emoji/unicode-glyph icon characters in the sidebar', () => {
    stubMobile(false)
    stubVersion('0.4.0')
    renderShell()
    const rail = document.querySelector('.rail')
    expect(rail).not.toBeNull()
    expect(rail?.textContent).not.toMatch(GLYPH_OR_EMOJI)
  })

  it('uses no emoji/unicode-glyph icon characters in the content header', () => {
    stubMobile(false)
    stubVersion('')
    renderShell()
    const header = screen.getByTestId('global-search-form').closest('header')
    expect(header).not.toBeNull()
    expect(header?.textContent).not.toMatch(GLYPH_OR_EMOJI)
  })

  it('uses no emoji/unicode-glyph icon characters in the mobile drawer', async () => {
    stubMobile(true)
    stubVersion('')
    const user = userEvent.setup()
    renderShell()
    await user.click(await screen.findByTestId('nav-toggle'))
    const drawer = await screen.findByTestId('nav-drawer')
    expect(drawer.textContent).not.toMatch(GLYPH_OR_EMOJI)
  })
})

// ---- version pill label ----

describe('versionPillLabel', () => {
  it('hides the pill for an empty version', () => {
    expect(versionPillLabel('')).toBeNull()
  })

  it('labels dev builds as Develop and releases as Stable', () => {
    expect(versionPillLabel('dev')).toBe('Develop')
    expect(versionPillLabel('develop-abc1234')).toBe('Develop')
    expect(versionPillLabel('0.4.0')).toBe('Stable')
  })
})
