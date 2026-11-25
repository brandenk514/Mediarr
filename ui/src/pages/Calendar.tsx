import { useCallback, useEffect, useState } from 'react'
import { client } from '../api/client'
import type { CalendarEntry } from '../api/types'
import { EmptyState, TbButton, Toolbar } from '../components/ui'

// The five Sonarr calendar views (P1 established the toolbar + view toggle;
// P4 wires the data + the iCal feed).
const VIEWS = ['Month', 'Week', 'Forecast', 'Day', 'Agenda'] as const
type CalendarView = (typeof VIEWS)[number]

// #121: the calendar is unified across kinds. CalendarEntry.kind is
// 'tv' | 'movie' | 'music' — books are excluded on purpose: the books
// domain has no release-date data (editions carry Year only), so a books
// calendar would need a data-model + API change (see issue #121 scoping).
export type CalendarKind = 'tv' | 'movie' | 'music'

// A kind filter is one kind, or 'all' for the merged feed. The unified
// default (/calendar) starts at 'all'; the /movies/calendar alias is the
// same page with the filter pre-set to 'movie' (#119 alias precedent).
export type CalendarFilter = 'all' | CalendarKind

// #121: the kind filter chips on the unified calendar.
const KIND_FILTERS: { value: CalendarFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'tv', label: 'TV' },
  { value: 'movie', label: 'Movies' },
  { value: 'music', label: 'Music' },
]

// Radarr v6 movie status chips for the movie calendar (the same chip values
// as the Movies list; the calendar only lists monitored movies, so the
// unmonitored variants can never appear).
const MOVIE_STATUS_CHIPS: { value: string; label: string }[] = [
  { value: 'downloaded:monitored', label: 'Downloaded (Monitored)' },
  { value: 'missing:monitored', label: 'Missing (Monitored)' },
  { value: 'queued', label: 'Queued' },
  { value: 'unreleased', label: 'Unreleased' },
]

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

// #121: merge the per-kind feed chunks into one list sorted by air start.
// The comparison is a plain string compare on the RFC3339 (UTC) instants, so
// it is deterministic and locale-free; ties break on kind then series_title
// so equal instants still render in a stable order.
export function mergeCalendarEntries(chunks: CalendarEntry[][]): CalendarEntry[] {
  return chunks
    .flat()
    .sort((a, b) =>
      a.air_start === b.air_start
        ? a.kind === b.kind
          ? a.series_title < b.series_title
            ? -1
            : 1
          : a.kind < b.kind
            ? -1
            : 1
        : a.air_start < b.air_start
          ? -1
          : 1,
    )
}

// ---- row + view renderers -------------------------------------------------

// The secondary text spans differ by kind: TV rows carry the episode title +
// SxE; movie rows carry the status + year; music rows carry the artist +
// album type (the album title itself is the row's main text — the backend
// sets SeriesTitle to the album title for kind=music entries). The row shape
// is shared, so the five views stay kind-agnostic.
export function entryTitle(e: CalendarEntry): string {
  switch (e.kind) {
    case 'movie':
      return e.status ?? ''
    case 'music':
      return e.artist_title ?? ''
    default:
      return e.episode_title
  }
}

export function entryCode(e: CalendarEntry): string {
  switch (e.kind) {
    case 'movie':
      return e.year ? String(e.year) : ''
    case 'music':
      return e.album_type ?? ''
    default:
      return e.sxe
  }
}

function EntryRow({ e }: { e: CalendarEntry }) {
  // show → episode title → SxE (absolute) → time range
  return (
    <div className="cal-episode-row" data-testid={`calendar-episode-row-${e.air_start}`}>
      <span className="cal-ep-show">{e.series_title}</span>
      <span className="cal-ep-title">{entryTitle(e)}</span>
      <span className="cal-ep-sxe">{entryCode(e)}</span>
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
            <EntryRow key={e.air_start} e={e} />
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
            <EntryRow e={e} />
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
              <EntryRow key={e.air_start} e={e} />
            ))}
          </div>
        )
      })}
    </div>
  )
}

function DayView({ entries, selected, filter }: { entries: CalendarEntry[]; selected: Date; filter: CalendarFilter }) {
  const items = entriesForDay(entries, selected)
  return (
    <div className="cal-day" data-testid="calendar-day">
      <div className="cal-day-head">{formatDayHeader(selected)}</div>
      {items.length === 0 ? (
        <p className="dim">
          {filter === 'tv'
            ? 'No episodes this day.'
            : filter === 'all'
              ? 'No items this day.'
              : 'No releases this day.'}
        </p>
      ) : (
        items.map((e) => <EntryRow key={e.air_start} e={e} />)
      )}
    </div>
  )
}

function MonthView({ entries, selected }: { entries: CalendarEntry[]; selected: Date }) {
  // A 6x7 month grid for the selected day's month; each cell lists SxEs (TV),
  // years (movie), or album types (music) per entry.
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
                {entryCode(e)}
              </span>
            ))}
          </div>
        )
      })}
    </div>
  )
}

// ---- page -----------------------------------------------------------------

export function CalendarPage({ kind = 'all' }: { kind?: CalendarFilter }) {
  const [view, setView] = useState<CalendarView>('Week')
  const [entries, setEntries] = useState<CalendarEntry[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Date>(new Date())
  const [showIcal, setShowIcal] = useState(false)
  const [chip, setChip] = useState<string | null>(null)
  // #121: the active kind filter — UI-local state, pre-set by the route for
  // the /movies/calendar alias (kind="movie").
  const [activeKind, setActiveKind] = useState<CalendarFilter>(kind)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const now = new Date()
      const end = new Date(now)
      end.setDate(end.getDate() + WINDOW_DAYS)
      const base = { start: toRFC3339(now), end: toRFC3339(end) }
      // #121: 'all' fans out over the three calendar kinds in parallel and
      // merges; a specific kind is a single request (the /movies/calendar
      // alias keeps its single kind=movie request).
      const rows =
        kind === 'all'
          ? mergeCalendarEntries(
              await Promise.all([
                client.listCalendar({ ...base, kind: 'tv' }),
                client.listCalendar({ ...base, kind: 'movie' }),
                client.listCalendar({ ...base, kind: 'music' }),
              ]),
            )
          : mergeCalendarEntries([await client.listCalendar({ ...base, kind })])
      setEntries(rows)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load calendar')
    } finally {
      setLoading(false)
    }
  }, [kind])

  useEffect(() => {
    void load()
  }, [load])

  // #121: the iCal feeds remain per-kind (no kind=all feed exists). The
  // unqualified feed is the TV feed (the server default), so 'all' and 'tv'
  // map to it and the TV URL stays byte-identical to pre-#121; the movie and
  // music filters swap in their kind's feed.
  const icalUrl =
    activeKind === 'movie'
      ? client.calendarICS({ kind: 'movie' })
      : activeKind === 'music'
        ? client.calendarICS({ kind: 'music' })
        : client.calendarICS()

  // #121: the kind chips filter the merged feed client-side; the Radarr
  // movie status chips then narrow the movie rows (movie filter active).
  const kindVisible = entries?.filter((e) => activeKind === 'all' || e.kind === activeKind) ?? null
  const visible = activeKind === 'movie' && chip ? kindVisible?.filter((e) => e.status === chip) ?? [] : kindVisible

  const emptyTitle =
    activeKind === 'tv'
      ? `No upcoming episodes in ${view} view`
      : activeKind === 'all'
        ? `No upcoming items in ${view} view`
        : `No upcoming releases in ${view} view`
  const emptyMessage =
    activeKind === 'tv'
      ? 'Upcoming monitored air dates from provider metadata appear here.'
      : activeKind === 'all'
        ? 'Upcoming monitored air dates and release dates from provider metadata appear here.'
        : 'Upcoming monitored release dates from provider metadata appear here.'

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

      {/* #121: the kind filter chips (All/TV/Movies/Music) on the unified
          calendar. The /movies/calendar alias renders this same row with the
          Movies chip pre-set. No books chip: the books domain has no
          release-date data. */}
      <div className="status-chips" data-testid="calendar-kind-chips">
        {KIND_FILTERS.map((f) => (
          <button
            key={f.value}
            type="button"
            className={`chip ${activeKind === f.value ? 'active' : ''}`}
            data-testid={`calendar-kind-${f.value}`}
            onClick={() => {
              setActiveKind(f.value)
              setChip(null)
            }}
          >
            {f.label}
          </button>
        ))}
      </div>

      {/* The Radarr movie status chips filter the movie rows — visible while
          the Movies filter is active (the /movies/calendar alias by default). */}
      {activeKind === 'movie' && (
        <div className="status-chips" data-testid="calendar-status-chips">
          {MOVIE_STATUS_CHIPS.map((c) => (
            <button
              key={c.value}
              type="button"
              className={`chip ${chip === c.value ? 'active' : ''}`}
              data-testid={`calendar-chip-${c.value.replace(/[:()]/g, '-').toLowerCase()}`}
              onClick={() => setChip(chip === c.value ? null : c.value)}
            >
              {c.label}
            </button>
          ))}
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
      ) : !visible || visible.length === 0 ? (
        <EmptyState
          title={emptyTitle}
          message={emptyMessage}
          testId="calendar-empty"
        />
      ) : view === 'Month' ? (
        <MonthView entries={visible} selected={selected} />
      ) : view === 'Week' ? (
        <WeekView entries={visible} selected={selected} />
      ) : view === 'Forecast' ? (
        <ForecastView entries={visible} />
      ) : view === 'Day' ? (
        <DayView entries={visible} selected={selected} filter={activeKind} />
      ) : (
        <AgendaView entries={visible} />
      )}
    </div>
  )
}
