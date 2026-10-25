import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { EventsPanel } from './EventsPanel'

vi.mock('../../api/client', () => ({
  ApiError: class extends Error {},
  client: {
    getSystemEvents: vi.fn(),
  },
}))

const EVENTS = {
  events: [
    {
      kind: 'series',
      parent_id: 11,
      name: 'Severance',
      event: 'Import',
      detail: 'S01E01 — Imported from media import',
      at: '2026-10-25T01:00:00Z',
    },
    {
      kind: 'movie',
      parent_id: 7,
      name: 'Dune',
      event: 'Monitored',
      detail: 'season 1 monitored',
      at: '2026-10-25T00:30:00Z',
    },
  ],
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemEvents as ReturnType<typeof vi.fn>).mockResolvedValue(EVENTS)
})

describe('EventsPanel', () => {
  it('renders the event log rows (kind pill, event, name, detail)', async () => {
    render(<EventsPanel />)
    const row = await screen.findByTestId('event-row-0')
    expect(row).toHaveTextContent('Severance')
    expect(row).toHaveTextContent('Import')
    expect(screen.getByTestId('event-row-1')).toHaveTextContent('Dune')
    expect(screen.getByTestId('system-events')).toBeInTheDocument()
  })

  it('filters by event type through the endpoint', async () => {
    const user = userEvent.setup()
    render(<EventsPanel />)
    await screen.findByTestId('event-row-0')

    await user.type(screen.getByTestId('events-filter'), 'Import')
    await user.click(screen.getByTestId('events-filter-btn'))
    await vi.waitFor(() =>
      expect(client.getSystemEvents).toHaveBeenLastCalledWith({ event: 'Import', limit: 100 }),
    )
  })

  it('clears the filter and reloads the full feed', async () => {
    const user = userEvent.setup()
    render(<EventsPanel />)
    await screen.findByTestId('event-row-0')

    await user.type(screen.getByTestId('events-filter'), 'Import')
    await user.click(screen.getByTestId('events-filter-btn'))
    await screen.findByTestId('events-clear-filter')
    await user.click(screen.getByTestId('events-clear-filter'))
    await vi.waitFor(() =>
      expect(client.getSystemEvents).toHaveBeenLastCalledWith({ limit: 100 }),
    )
  })

  it('shows the error-gated state when the events endpoint fails (P1 dev stack)', async () => {
    ;(client.getSystemEvents as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Not Found'))
    render(<EventsPanel />)
    await screen.findByTestId('system-events-error')
    expect(screen.getByText('Events unavailable')).toBeInTheDocument()
  })
})
