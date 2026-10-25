import { useState } from 'react'
import { Toolbar, TbButton, EmptyState } from '../components/ui'

// The five Sonarr calendar views. P1 establishes the toolbar + view toggle;
// the actual calendar grid + iCal feed ship with the Calendar build (P4).
const VIEWS = ['Month', 'Week', 'Forecast', 'Day', 'Agenda'] as const
type CalendarView = (typeof VIEWS)[number]

export function CalendarPage() {
  const [view, setView] = useState<CalendarView>('Week')

  return (
    <div className="list-page" data-testid="calendar">
      <div className="page-head">
        <h2>Calendar</h2>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="🔗" label="iCal Link" testId="calendar-ical" disabled />
            <TbButton icon="⟳" label="RSS Sync" testId="calendar-rss" disabled divider />
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
                onClick={() => setView(v)}
                testId={`calendar-view-${v.toLowerCase()}`}
              />
            ))}
          </>
        }
      />
      <EmptyState
        title={`No upcoming episodes in ${view} view`}
        message="The calendar (Month / Week / Forecast / Day / Agenda) and iCal feed land with the Calendar build (P4)."
        testId="calendar-empty"
      />
    </div>
  )
}
