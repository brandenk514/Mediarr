import { useCallback, useEffect, useState } from 'react'
import { client } from '../api/client'
import type { CalendarEntry } from '../api/types'
import { EmptyState, TbButton, Toolbar } from '../components/ui'

// The five Sonarr calendar views (P1 established the toolbar + view toggle;
// P4 wires the data + the iCal feed).
const VIEWS = ['Month', 'Week', 'Forecast', 'Day', 'Agenda'] as const
type CalendarView = (typeof VIEWS)[number]

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const

// The default look-ahead window when no explicit window is supplied; mirrors
// the backend's today..today+30d default so the UI and server agree.
const WINDOW_DAYS = 30

// ---- pure, deterministic helpers (unit-testable) -------------------------

// "HH:MM–HH:MM" range from the air start/end instants (UTC, so it is stable
// across locales/timezones in tests).
export function formatTimeRange(startIso: string, endIso: string): string {
  const hm = (iso: string) => {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return ''
    return `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`
  }
  return `${hm(startIso)}–${hm(endIso)}`
}

// RFC3339 (UTC) instant for a Date — used to build the request window.
export function toRFC3339(d: Date): string {
  return d.toISOString()
}

// Local Monday-free Sunday start of the week for a given date.
export function startOfWeek(d: Date): Date {
  const out = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  out.setDate(d.getDate() - d.getDay())
  return out
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// Local day key for an entry (derived from its air start instant).
function dayKeyOf(e: CalendarEntry): string {
  return isoDay(new Date(e.air_start))
}

function entriesForDay(entries: CalendarEntry[], day: Date): CalendarEntry[] {
  const key = isoDay(day)
  return entries.filter((e) => dayKeyOf(e) === key)
}

// Day-header label, e.g. "Tue, Oct 27".
export function formatDayHeader(d: Date): string {
  return d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
}

// ---- row + view renderers -------------------------------------------------

function EpisodeRow({ e }: { e: CalendarEntry }) {
  // show → episode title → SxE (absolute) → time range
  return (
    <div className="cal-episode-row" data-testid={`calendar-episode-row-${e.air_start}`}>
      <span className="cal-ep-show">{e.series_title}</span>
      <span className="cal-ep-title">{e.episode_title}</span>
      <span className="cal-ep-sxe">{e.sxe}</span>
      <span className="cal-ep-time">{formatTimeRange(e.air_start, e.air_end)}</span>
    </div>
  )
}

function AgendaView({ entries }: { entries: CalendarEntry[] }) {
  // Flat chronological list grouped by local day.
  const groups: [string, CalendarEntry[]][] = []
  for (const e of entries) {
    const key = dayKeyOf(e)
    const g = groups.find(([k]) => k === key)
    if (g) g[1].push(e)
    else groups.push([key, [e]])
  }
  return (
    <div className="cal-agenda" data-testid="calendar-agenda">
      {groups.map(([key, items]) => (
        <div key={key} className="cal-agenda-day">
          <div className="cal-day-head">{formatDayHeader(new Date(`${key}T12:00:00`))}</div>
          {items.map((e) => (
            <EpisodeRow key={e.air_start} e={e} />
          ))}
        </div>
      ))}
    </div>
  )
}

function ForecastView({ entries }: { entries: CalendarEntry[] }) {
  // The next upcoming episodes as an ordered list (each row carries its date).
  return (
    <div className="cal-forecast" data-testid="calendar-forecast">
      <ul className="cal-forecast-list">
        {entries.map((e) => (
          <li key={e.air_start} className="cal-forecast-item">
            <EpisodeRow e={e} />
            <span className="cal-forecast-date dim">{formatDayHeader(new Date(e.air_start))}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function WeekView({ entries, selected }: { entries: CalendarEntry[]; selected: Date }) {
  // Seven columns, one per weekday of the selected day's week.
  const weekStart = startOfWeek(selected)
  return (
    <div className="cal-week" data-testid="calendar-week">
      {WEEKDAYS.map((wd, i) => {
        const day = new Date(weekStart)
        day.setDate(weekStart.getDate() + i)
        const items = entriesForDay(entries, day)
        return (
          <div key={wd} className="cal-week-col" data-testid={`calendar-week-col-${wd.toLowerCase()}`}>
            <div className="cal-week-col-head">
              {wd} {day.getDate()}
            </div>
            {items.map((e) => (
              <EpisodeRow key={e.air_start} e={e} />
            ))}
          </div>
        )
      })}
    </div>
  )
}

function DayView({ entries, selected }: { entries: CalendarEntry[]; selected: Date }) {
  const items = entriesForDay(entries, selected)
  return (
    <div className="cal-day" data-testid="calendar-day">
      <div className="cal-day-head">{formatDayHeader(selected)}</div>
      {items.length === 0 ? (
        <p className="dim">No episodes this day.</p>
      ) : (
        items.map((e) => <EpisodeRow key={e.air_start} e={e} />)
      )}
    </div>
  )
}

function MonthView({ entries, selected }: { entries: CalendarEntry[]; selected: Date }) {
  // A 6x7 month grid for the selected day's month; each cell lists SxEs.
  const first = new Date(selected.getFullYear(), selected.getMonth(), 1)
  const gridStart = startOfWeek(first)
  const cells: { day: Date; inMonth: boolean }[] = []
  for (let i = 0; i < 42; i++) {
    const day = new Date(gridStart)
    day.setDate(gridStart.getDate() + i)
    cells.push({ day, inMonth: day.getMonth() === selected.getMonth() })
  }
  return (
    <div className="cal-month" data-testid="calendar-month">
      {WEEKDAYS.map((wd) => (
        <div key={wd} className="cal-month-dow">{wd}</div>
      ))}
      {cells.map(({ day, inMonth }) => {
        const items = entriesForDay(entries, day)
        return (
          <div
            key={isoDay(day)}
            className={`cal-month-cell${inMonth ? '' : ' cal-out'}`}
            data-testid={`calendar-month-cell-${isoDay(day)}`}
          >
            <span className="cal-month-num">{day.getDate()}</span>
            {items.map((e) => (
              <span key={e.air_start} className="cal-month-sxe">
                {e.sxe}
              </span>
            ))}
          </div>
        )
      })}
    </div>
  )
}

// ---- page -----------------------------------------------------------------

export function CalendarPage() {
  const [view, setView] = useState<CalendarView>('Week')
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Date>(new Date())
  const [showIcal, setShowIcal] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const now = new Date()
      const end = new Date(now)
      end.setDate(end.getDate() + WINDOW_DAYS)
      const rows = await client.listCalendar({
        start: toRFC3339(now),
        end: toRFC3339(end),
      })
      setEntries(rows)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load calendar')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const icalUrl = client.calendarICS()

  // Week strip always reflects the selected day's week (Sun–Sat + Today).
  const stripStart = startOfWeek(selected)

  return (
    <div className="list-page" data-testid="calendar">
      <div className="page-head">
        <h2>Calendar</h2>
      </div>
      <Toolbar
        left={
          <>
            <TbButton
              icon="🔗"
              label="iCal Link"
              testId="calendar-ical"
              onClick={() => setShowIcal((v) => !v)}
            />
            <TbButton icon="⟳" label="RSS Sync" testId="calendar-rss" disabled divider />
            <TbButton icon="🔎" label="Search for Missing" testId="calendar-search-missing" disabled divider />
            <TbButton icon="⚙" label="Options" testId="calendar-options" disabled />
            <TbButton icon="⏷" label="Filter" testId="calendar-filter" disabled divider />
          </>
        }
        right={
          <>
            {VIEWS.map((v, i) => (
              <TbButton
                key={v}
                icon=""
                label={v}
                divider={i === 0}
                title={view === v ? `${v} (active)` : v}
                onClick={() => setView(v)}
                testId={`calendar-view-${v.toLowerCase()}`}
              />
            ))}
          </>
        }
      />

      {showIcal && (
        <div className="card" data-testid="calendar-ical-bar">
          <span className="dim">iCal feed (add to Google/Apple Calendar): </span>
          <input
            readOnly
            value={icalUrl}
            data-testid="calendar-ical-url"
            style={{ width: '100%' }}
          />
        </div>
      )}

      {/* Week strip: Sun–Sat + Today (chrome, always shown). */}
      <div className="cal-weekstrip" data-testid="calendar-weekstrip">
        <button type="button" className="tb-btn" data-testid="calendar-today" onClick={() => setSelected(new Date())}>
          Today
        </button>
        {WEEKDAYS.map((wd, i) => {
          const day = new Date(stripStart)
          day.setDate(stripStart.getDate() + i)
          const isSel = sameDay(day, selected)
          return (
            <button
              key={wd}
              type="button"
              className={`tb-btn${isSel ? ' tb-active' : ''}`}
              data-testid={`calendar-day-${wd.toLowerCase()}`}
              onClick={() => setSelected(day)}
            >
              <span>{wd}</span>
              <span className="dim">{day.getDate()}</span>
            </button>
          )
        })}
      </div>

      {/* Error-gated body: loading / empty / view render ONLY when there is no
          request error; a failed request shows ONLY the error state. */}
      {error ? (
        <div className="card" data-testid="calendar-error">
          <p className="error" data-testid="calendar-error-message">
            {error}
          </p>
          <button onClick={() => void load()}>Retry</button>
        </div>
      ) : loading && !entries ? (
        <p className="dim">Loading calendar…</p>
      ) : !entries || entries.length === 0 ? (
        <EmptyState
          title={`No upcoming episodes in ${view} view`}
          message="Upcoming monitored air dates from provider metadata appear here."
          testId="calendar-empty"
        />
      ) : view === 'Month' ? (
        <MonthView entries={entries} selected={selected} />
      ) : view === 'Week' ? (
        <WeekView entries={entries} selected={selected} />
      ) : view === 'Forecast' ? (
        <ForecastView entries={entries} />
      ) : view === 'Day' ? (
        <DayView entries={entries} selected={selected} />
      ) : (
        <AgendaView entries={entries} />
      )}
    </div>
  )
}
