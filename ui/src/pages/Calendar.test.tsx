import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import type { CalendarEntry } from '../api/types'
import { CalendarPage } from './Calendar'

vi.mock('../api/client', () => ({
  client: {
    listCalendar: vi.fn(),
    calendarICS: vi.fn(),
  },
}))

function fixtures(): CalendarEntry[] {
  return [
    {
      kind: 'tv',
      series_id: 1,
      series_title: 'Severance',
      season: 1,
      episode: 5,
      episode_title: 'The Department',
      sxe: 'S1E05',
      air_date: '2026-10-27',
      air_start: '2026-10-27T22:00:00Z',
      air_end: '2026-10-27T22:50:00Z',
    },
    {
      kind: 'tv',
      series_id: 2,
      series_title: 'Slow Horses',
      season: 2,
      episode: 1,
      episode_title: 'A Question of Trust',
      sxe: 'S2E01',
      air_date: '2026-10-28',
      air_start: '2026-10-28T20:30:00Z',
      air_end: '2026-10-28T21:15:00Z',
    },
  ]
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/calendar']}>
      <CalendarPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listCalendar).mockReset()
  vi.mocked(client.calendarICS).mockReset()
  vi.mocked(client.calendarICS).mockReturnValue('/calendar.ics?token=***')
})

describe('CalendarPage (P4)', () => {
  it('renders all five views (Month / Week / Forecast / Day / Agenda)', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(fixtures() as never)
    renderPage()
    // Default view is Week.
    expect(await screen.findByTestId('calendar-week')).toBeInTheDocument()
    const views: [string, string][] = [
      ['calendar-view-month', 'calendar-month'],
      ['calendar-view-week', 'calendar-week'],
      ['calendar-view-forecast', 'calendar-forecast'],
      ['calendar-view-day', 'calendar-day'],
      ['calendar-view-agenda', 'calendar-agenda'],
    ]
    for (const [btn, container] of views) {
      await user.click(screen.getByTestId(btn))
      expect(await screen.findByTestId(container)).toBeInTheDocument()
    }
  })

  it('shows the week strip (Sun–Sat) plus a Today button', async () => {
    vi.mocked(client.listCalendar).mockResolvedValue(fixtures() as never)
    renderPage()
    await screen.findByTestId('calendar-week')
    expect(screen.getByTestId('calendar-weekstrip')).toBeInTheDocument()
    for (const wd of ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']) {
      expect(screen.getByTestId(`calendar-day-${wd}`)).toBeInTheDocument()
    }
    expect(screen.getByTestId('calendar-today')).toBeInTheDocument()
  })

  it('shows per-day episode rows: show -> title -> SxE -> time range', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(fixtures() as never)
    renderPage()
    // Agenda lists every entry (grouped by day), independent of the selected week.
    await user.click(screen.getByTestId('calendar-view-agenda'))
    const row = await screen.findByTestId('calendar-episode-row-2026-10-27T22:00:00Z')
    expect(row).toHaveTextContent('Severance')
    expect(row).toHaveTextContent('The Department')
    expect(row).toHaveTextContent('S1E05')
    expect(row).toHaveTextContent('22:00')
    expect(row).toHaveTextContent('22:50')
  })

  it('reveals the iCal feed link (token-authed feed URL)', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(fixtures() as never)
    renderPage()
    await screen.findByTestId('calendar-week')
    await user.click(screen.getByTestId('calendar-ical'))
    expect(await screen.findByTestId('calendar-ical-bar')).toBeInTheDocument()
    expect(screen.getByTestId('calendar-ical-url')).toHaveValue('/calendar.ics?token=***')
  })

  it('shows the empty state when there are no upcoming episodes', async () => {
    vi.mocked(client.listCalendar).mockResolvedValue([])
    renderPage()
    expect(await screen.findByTestId('calendar-empty')).toBeInTheDocument()
  })

  it('shows ONLY the error state on a failed request (no empty / no rows)', async () => {
    vi.mocked(client.listCalendar).mockRejectedValue(new Error('boom'))
    renderPage()
    await screen.findByTestId('calendar-error')
    expect(screen.getByTestId('calendar-error-message')).toHaveTextContent('boom')
    // Error-gating: the empty state and any view body must NOT render.
    expect(screen.queryByTestId('calendar-empty')).not.toBeInTheDocument()
    expect(screen.queryByTestId('calendar-week')).not.toBeInTheDocument()
    expect(screen.queryByTestId('calendar-episode-row-2026-10-27T22:00:00Z')).not.toBeInTheDocument()
  })
})
