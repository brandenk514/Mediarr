import { render, screen, waitFor } from '@testing-library/react'
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

  it('TV page has no status chips (regression: the chip row is movie-only)', async () => {
    vi.mocked(client.listCalendar).mockResolvedValue(fixtures() as never)
    renderPage()
    await screen.findByTestId('calendar-week')
    expect(screen.queryByTestId('calendar-status-chips')).not.toBeInTheDocument()
  })
})

function movieFixtures(): CalendarEntry[] {
  return [
    {
      kind: 'movie',
      series_id: 11,
      series_title: 'Dune: Part Three',
      season: 0,
      episode: 0,
      episode_title: '',
      sxe: '',
      air_date: '2026-11-04',
      air_start: '2026-11-04T00:00:00Z',
      air_end: '2026-11-04T23:59:00Z',
      year: 2026,
      status: 'unreleased',
      quality_profile: 'HD-1080p',
    },
    {
      kind: 'movie',
      series_id: 12,
      series_title: 'Blade Runner 2099',
      season: 0,
      episode: 0,
      episode_title: '',
      sxe: '',
      air_date: '2026-11-05',
      air_start: '2026-11-05T00:00:00Z',
      air_end: '2026-11-05T23:59:00Z',
      year: 2026,
      status: 'queued',
    },
  ]
}

function renderMoviePage() {
  return render(
    <MemoryRouter initialEntries={['/movies/calendar']}>
      <CalendarPage kind="movie" />
    </MemoryRouter>,
  )
}

describe('CalendarPage kind=movie (R4 movie calendar)', () => {
  it('renders all five views for movie entries', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(movieFixtures() as never)
    renderMoviePage()
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

  it('requests kind=movie and exposes the movie iCal feed URL', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(movieFixtures() as never)
    vi.mocked(client.calendarICS).mockReturnValue('/calendar.ics?kind=movie&token=***')
    renderMoviePage()
    await screen.findByTestId('calendar-week')
    expect(vi.mocked(client.listCalendar)).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'movie' }),
    )
    await user.click(screen.getByTestId('calendar-ical'))
    expect(await screen.findByTestId('calendar-ical-bar')).toBeInTheDocument()
    expect(screen.getByTestId('calendar-ical-url')).toHaveValue('/calendar.ics?kind=movie&token=***')
  })

  it('movie rows show title, status, year and the all-day range', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(movieFixtures() as never)
    renderMoviePage()
    // Agenda lists every entry, independent of the selected week.
    await user.click(screen.getByTestId('calendar-view-agenda'))
    const row = await screen.findByTestId('calendar-episode-row-2026-11-04T00:00:00Z')
    expect(row).toHaveTextContent('Dune: Part Three')
    expect(row).toHaveTextContent('unreleased')
    expect(row).toHaveTextContent('2026')
    // All-day event: 00:00–23:59.
    expect(row).toHaveTextContent('00:00')
    expect(row).toHaveTextContent('23:59')
    // Movie entries carry no SxE.
    expect(row).not.toHaveTextContent('S1E')
  })

  it('shows the status chips and filters entries by status', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listCalendar).mockResolvedValue(movieFixtures() as never)
    renderMoviePage()
    await user.click(screen.getByTestId('calendar-view-agenda'))
    expect(await screen.findByTestId('calendar-episode-row-2026-11-04T00:00:00Z')).toBeInTheDocument()
    expect(screen.getByTestId('calendar-episode-row-2026-11-05T00:00:00Z')).toBeInTheDocument()
    // Four Radarr movie status chips (unmonitored variants never appear).
    for (const chip of ['downloaded-monitored', 'missing-monitored', 'queued', 'unreleased']) {
      expect(screen.getByTestId(`calendar-chip-${chip}`)).toBeInTheDocument()
    }
    // Filter to queued: the unreleased row disappears, the queued row stays.
    await user.click(screen.getByTestId('calendar-chip-queued'))
    expect(screen.queryByTestId('calendar-episode-row-2026-11-04T00:00:00Z')).not.toBeInTheDocument()
    expect(screen.getByTestId('calendar-episode-row-2026-11-05T00:00:00Z')).toBeInTheDocument()
    // Click again clears the filter.
    await user.click(screen.getByTestId('calendar-chip-queued'))
    expect(await screen.findByTestId('calendar-episode-row-2026-11-04T00:00:00Z')).toBeInTheDocument()
  })

  it('shows the empty state when there are no upcoming releases', async () => {
    vi.mocked(client.listCalendar).mockResolvedValue([])
    renderMoviePage()
    expect(await screen.findByTestId('calendar-empty')).toBeInTheDocument()
    expect(screen.getByTestId('calendar-empty')).toHaveTextContent('No upcoming releases')
  })
})

// ---- #121: unified cross-kind calendar ------------------------------------

function unifiedFixture(): { tv: CalendarEntry[]; movie: CalendarEntry[]; music: CalendarEntry[] } {
  return {
    // One entry per kind, staggered so the merged sort order is observable:
    // movie (11-04) < tv (11-05) < music (11-06) — while the fetch fan-out
    // returns the tv chunk first.
    tv: [
      {
        kind: 'tv',
        series_id: 1,
        series_title: 'Severance',
        season: 1,
        episode: 5,
        episode_title: 'The Department',
        sxe: 'S1E05',
        air_date: '2026-11-05',
        air_start: '2026-11-05T22:00:00Z',
        air_end: '2026-11-05T22:50:00Z',
      },
    ],
    movie: [
      {
        kind: 'movie',
        series_id: 11,
        series_title: 'Dune: Part Three',
        season: 0,
        episode: 0,
        episode_title: '',
        sxe: '',
        air_date: '2026-11-04',
        air_start: '2026-11-04T00:00:00Z',
        air_end: '2026-11-04T23:59:00Z',
        year: 2026,
        status: 'unreleased',
        quality_profile: 'HD-1080p',
      },
    ],
    music: [
      {
        kind: 'music',
        series_id: 21,
        series_title: 'A Moon Shaped Pool',
        season: 0,
        episode: 0,
        episode_title: '',
        sxe: '',
        air_date: '2026-11-06',
        air_start: '2026-11-06T00:00:00Z',
        air_end: '2026-11-06T23:59:00Z',
        artist_id: 5,
        artist_title: 'Radiohead',
        album_id: 21,
        album_title: 'A Moon Shaped Pool',
        album_type: 'album',
      },
    ],
  }
}

// The unified default view fans out one listCalendar call per kind
// (tv/move/music); the mock answers each call with that kind's chunk.
function mockUnified(tv: CalendarEntry[], movie: CalendarEntry[], music: CalendarEntry[]) {
  vi.mocked(client.listCalendar).mockImplementation((f) => {
    if (f?.kind === 'movie') return Promise.resolve(movie as never)
    if (f?.kind === 'music') return Promise.resolve(music as never)
    return Promise.resolve(tv as never)
  })
}

describe('CalendarPage unified (issue #121)', () => {
  it('merged default view shows tv + movie + music rows sorted by air start', async () => {
    const user = userEvent.setup()
    const fx = unifiedFixture()
    mockUnified(fx.tv, fx.movie, fx.music)
    renderPage()
    // Agenda lists every entry (grouped by day), independent of the selected week.
    await user.click(screen.getByTestId('calendar-view-agenda'))
    const rows = await screen.findAllByTestId(/calendar-episode-row-/)
    expect(rows).toHaveLength(3)
    // Across kinds the rows are ordered by air start: movie (11-04), tv
    // (11-05), music (11-06) — even though the fan-out returns tv first.
    expect(rows.map((r) => r.querySelector('.cal-ep-show')?.textContent)).toEqual([
      'Dune: Part Three',
      'Severance',
      'A Moon Shaped Pool',
    ])
  })

  it('kind filter chips (All/TV/Movies/Music) filter the merged feed', async () => {
    const user = userEvent.setup()
    const fx = unifiedFixture()
    mockUnified(fx.tv, fx.movie, fx.music)
    renderPage()
    await user.click(screen.getByTestId('calendar-view-agenda'))
    await screen.findAllByTestId(/calendar-episode-row-/)
    for (const id of [
      'calendar-kind-all',
      'calendar-kind-tv',
      'calendar-kind-movie',
      'calendar-kind-music',
    ]) {
      expect(screen.getByTestId(id)).toBeInTheDocument()
    }
    await user.click(screen.getByTestId('calendar-kind-music'))
    await waitFor(() => expect(screen.getAllByTestId(/calendar-episode-row-/)).toHaveLength(1))
    expect(screen.getByTestId('calendar-episode-row-2026-11-06T00:00:00Z')).toBeInTheDocument()
    await user.click(screen.getByTestId('calendar-kind-all'))
    await waitFor(() => expect(screen.getAllByTestId(/calendar-episode-row-/)).toHaveLength(3))
  })

  it('music rows show the album title as main text plus artist and album type', async () => {
    const user = userEvent.setup()
    const fx = unifiedFixture()
    mockUnified([], [], fx.music)
    renderPage()
    await user.click(screen.getByTestId('calendar-view-agenda'))
    const row = await screen.findByTestId('calendar-episode-row-2026-11-06T00:00:00Z')
    // The album title is the row's main text (the backend sets SeriesTitle to
    // the album title for kind=music), and the artist + album type decorate it.
    expect(row).toHaveTextContent('A Moon Shaped Pool')
    expect(row).toHaveTextContent('Radiohead')
    expect(row).toHaveTextContent('album')
  })

  it('/movies/calendar alias: movie filter pre-set, movie-only rows, movie status chips', async () => {
    const user = userEvent.setup()
    const fx = unifiedFixture()
    mockUnified([], fx.movie, [])
    renderMoviePage()
    await user.click(screen.getByTestId('calendar-view-agenda'))
    await screen.findAllByTestId(/calendar-episode-row-/)
    // The alias is the unified calendar with the Movies chip pre-set.
    expect(screen.getByTestId('calendar-kind-movie')).toBeInTheDocument()
    expect(screen.getAllByTestId(/calendar-episode-row-/)).toHaveLength(1)
    expect(screen.getByTestId('calendar-episode-row-2026-11-04T00:00:00Z')).toBeInTheDocument()
    // The Radarr movie status chips stay visible for the alias.
    expect(screen.getByTestId('calendar-status-chips')).toBeInTheDocument()
  })
})
