import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { SystemPage } from './System'

// Mock the typed client. The Status page reads three endpoints in one
// Promise.all; Tasks reads one; the nav badge reads health.
vi.mock('../../api/client', () => ({
  ApiError: class extends Error {},
  client: {
    getSystemHealth: vi.fn(),
    getSystemDisk: vi.fn(),
    getSystemAbout: vi.fn(),
    getSystemTasks: vi.fn(),
    getSystemBackup: vi.fn(),
    putSystemBackup: vi.fn(),
    getSystemEvents: vi.fn(),
    getSystemUpdates: vi.fn(),
    runMigration: vi.fn(),
    getMigrationStatus: vi.fn(),
    openSystemLogsStream: vi.fn(() => () => {}),
  },
}))

const HEALTH = {
  checks: [
    { check: 'postgres', message: 'db reachable', severity: 'ok', wiki_link: '' },
    { check: 'redis', message: 'cache degraded', severity: 'warning', wiki_link: 'https://docs/x/redis' },
  ],
  counts: { ok: 1, warning: 1, error: 0 },
  ok: 1,
  warning: 1,
  error: 0,
}

const DISK = {
  roots: [
    {
      path: '/data/media',
      exists: true,
      total_bytes: 107374182400,
      used_bytes: 53687091200,
      usage_pct: 0.5,
    },
  ],
  configured: 1,
  available: 1,
}

const ABOUT = {
  version: '0.6.0',
  commit: 'abc1234',
  build_time: '2026-10-25T00:00:00Z',
  go_version: 'go1.27.2',
  mode: 'development',
  started_at: '2026-10-25T00:00:00Z',
  uptime_seconds: 3600,
  uptime: '1h 0m',
}

const TASKS = {
  tasks: [
    {
      name: 'indexer_health',
      description: 'Probe every enabled indexer',
      kind: 'scheduled',
      interval: '5m',
      enabled: true,
      running: false,
      last_run: '2026-10-25T01:00:00Z',
      last_duration: '120ms',
      last_error: '',
    },
  ],
  running: 0,
  scheduled: 1,
  total: 1,
}

function renderSystem(initial = '/system/status') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/system/:page" element={<SystemPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue(HEALTH)
  ;(client.getSystemDisk as ReturnType<typeof vi.fn>).mockResolvedValue(DISK)
  ;(client.getSystemAbout as ReturnType<typeof vi.fn>).mockResolvedValue(ABOUT)
  ;(client.getSystemTasks as ReturnType<typeof vi.fn>).mockResolvedValue(TASKS)
})

describe('System page — nav + Status sub-page', () => {
  it('renders the seven sub-page nav items', async () => {
    renderSystem()
    const nav = await screen.findByTestId('system-nav')
    for (const slug of ['status', 'tasks', 'backup', 'updates', 'events', 'logs', 'import']) {
      expect(screen.getByTestId(`system-nav-${slug}`)).toBeInTheDocument()
    }
    expect(nav).toBeInTheDocument()
  })

  it('renders the health table with severity pills and a wiki link', async () => {
    renderSystem()
    await screen.findByTestId('system-status')
    // postgres row is ok; redis row is warning and has a docs link.
    expect(screen.getByTestId('health-row-postgres')).toBeInTheDocument()
    expect(screen.getByTestId('health-row-redis')).toBeInTheDocument()
    expect(screen.getByTestId('health-row-redis')).toHaveTextContent('warning')
    const link = screen.getByTestId('health-link-redis')
    expect(link).toHaveAttribute('href', 'https://docs/x/redis')
  })

  it('renders disk space with usage and the About card', async () => {
    renderSystem()
    await screen.findByTestId('system-status')
    expect(screen.getByTestId('system-disk')).toBeInTheDocument()
    expect(screen.getByTestId('disk-row-datamedia')).toBeInTheDocument()
    expect(screen.getByTestId('system-about')).toBeInTheDocument()
    // About shows version + mode + uptime.
    const about = screen.getByTestId('system-about')
    expect(about).toHaveTextContent('0.6.0')
    expect(about).toHaveTextContent('development')
    expect(about).toHaveTextContent('1h 0m')
  })

  it('shows an error-gated state when the status endpoints 404 (P1 dev stack)', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Not Found'))
    renderSystem()
    await screen.findByTestId('system-status-error')
    expect(screen.getByText('Status unavailable')).toBeInTheDocument()
  })

  it('navigates to the Tasks sub-page and renders the worker inventory', async () => {
    const user = userEvent.setup()
    renderSystem()
    await screen.findByTestId('system-status')
    await user.click(screen.getByTestId('system-nav-tasks'))
    await waitFor(() => expect(screen.getByTestId('system-tasks')).toBeInTheDocument())
    expect(screen.getByTestId('task-row-indexer_health')).toBeInTheDocument()
    expect(screen.getByTestId('system-tasks')).toHaveTextContent('0 running')
    expect(screen.getByTestId('system-tasks')).toHaveTextContent('1 scheduled')
  })
})
