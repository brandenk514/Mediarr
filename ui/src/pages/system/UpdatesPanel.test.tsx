import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { UpdatesPanel } from './UpdatesPanel'

vi.mock('../../api/client', () => ({
  ApiError: class extends Error {},
  client: {
    getSystemUpdates: vi.fn(),
  },
}))

const UPDATES = {
  current: '0.6.0',
  changelog: [
    {
      version: '0.6.0',
      date: '2026-10-20',
      entries: ['System area: Status, Tasks, Backup, Events, Updates, Log Files.', 'Sonarr drop-in migration.'],
    },
    {
      version: '0.5.0',
      date: '2026-09-01',
      entries: ['Settings shell (13 sections).'],
    },
  ],
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemUpdates as ReturnType<typeof vi.fn>).mockResolvedValue(UPDATES)
})

describe('UpdatesPanel', () => {
  it('renders the running version and the changelog entries', async () => {
    render(<UpdatesPanel />)
    await screen.findByTestId('system-updates')
    expect(screen.getByTestId('updates-current-version')).toHaveTextContent('v0.6.0')
    expect(screen.getByTestId('changelog-entry-0.6.0')).toHaveTextContent('drop-in migration')
    expect(screen.getByTestId('changelog-entry-0.5.0')).toBeInTheDocument()
  })

  it('marks the entry matching the running version as current', async () => {
    render(<UpdatesPanel />)
    const current = await screen.findByTestId('changelog-entry-0.6.0')
    expect(current).toHaveTextContent('current')
    // The older release must not carry the badge.
    expect(screen.getByTestId('changelog-entry-0.5.0')).not.toHaveTextContent('current')
  })

  it('shows the error-gated state when the updates endpoint fails (P1 dev stack)', async () => {
    ;(client.getSystemUpdates as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Not Found'))
    render(<UpdatesPanel />)
    await screen.findByTestId('system-updates-error')
    expect(screen.getByText('Updates unavailable')).toBeInTheDocument()
  })
})
