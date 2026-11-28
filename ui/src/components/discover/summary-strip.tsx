// #151: the compact per-kind summary strip (the KPI counts that used to
// live on the retired Dashboard, issue #148 — folded into Discover per the
// #151 owner-approved IA). One stat per library kind (label, item count,
// wants / queue when non-zero, and a link into the kind's list page so the
// counts are actionable) plus a totals card.
import { Link } from 'react-router-dom'
import type { Dashboard } from '../../api/types'

const KIND_LIST_ROUTE: Record<string, string> = {
  movie: '/movies',
  tv: '/tv',
  music: '/music',
  book: '/books',
}

export function SummaryStrip({ dash, testId }: { dash: Dashboard; testId?: string }) {
  const { libraries, totals } = dash
  return (
    <div className="discover-kpi flex flex-wrap gap-2" data-testid={testId ?? 'discover-kpi'}>
      {libraries.map((l) => {
        const route = KIND_LIST_ROUTE[l.kind]
        return (
          <Link
            key={l.kind}
            to={route}
            className="flex min-w-28 flex-1 flex-col rounded-lg border border-border bg-surface px-3 py-2 transition-colors hover:border-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            data-testid={`discover-kpi-${l.kind}`}
          >
            <span className="text-2xs font-semibold uppercase tracking-wide text-text-muted">{l.label}</span>
            <span className="text-xl font-bold text-text">{l.count}</span>
            {(l.wants > 0 || l.queue > 0) && (
              <span className="text-2xs text-text-muted">
                {l.wants > 0 && <span>wants {l.wants}</span>}
                {l.wants > 0 && l.queue > 0 && ' · '}
                {l.queue > 0 && <span>queue {l.queue}</span>}
              </span>
            )}
          </Link>
        )
      })}
      <div className="flex min-w-32 flex-1 flex-col rounded-lg border border-border bg-surface-raised/40 px-3 py-2" data-testid="discover-kpi-total">
        <span className="text-2xs font-semibold uppercase tracking-wide text-text-muted">Total</span>
        <span className="text-xl font-bold text-text">{totals.libraries}</span>
        <span className="text-2xs text-text-muted">
          {totals.wants} want{totals.wants === 1 ? '' : 's'} · {totals.queue} in queue
        </span>
      </div>
    </div>
  )
}
