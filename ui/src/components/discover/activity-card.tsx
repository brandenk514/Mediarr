// #151: the landscape recent-activity card (the reference "Recent
// Requests" card: a darkened backdrop, a kind pill, the title, a status
// pill, and a date / source footer). The cards absorb the retired
// Dashboard's recent queue + recent history feeds (issue #148).
import { Badge, type BadgeTone } from '../primitives/badge'
import { posterInitials } from '../primitives/poster-card'
import { KIND_PILL } from './poster-cell'
import type { ActivityCard as ActivityCardModel } from '../../pages/discover/model'

export function ActivityCardView({ card, testId }: { card: ActivityCardModel; testId?: string }) {
  const pill = KIND_PILL[card.kind]
  const dateText = formatDay(card.date)

  return (
    <div
      className="discover-activity-card w-72 shrink-0 overflow-hidden rounded-lg border border-border bg-surface"
      data-testid={testId}
      title={card.title}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-surface-raised">
        {/* The wire types carry no backdrop art for queue/history items, so
            the backdrop is the initials stand-in, darkened so the text over
            it stays legible (the reference darkened-backdrop style). */}
        <span
          aria-hidden
          className="flex h-full w-full items-center justify-center text-2xl font-bold tracking-widest text-text-muted"
        >
          {posterInitials(card.title)}
        </span>
        <span className="absolute inset-0 bg-gradient-to-t from-bg/90 via-bg/40 to-transparent" aria-hidden />
        <span className="absolute left-1.5 top-1.5">
          <Badge tone={pill.tone}>{pill.label}</Badge>
        </span>
        <span className="absolute bottom-1.5 left-1.5 right-1.5 flex items-end justify-between gap-2">
          <span className="truncate text-sm font-semibold text-text">{card.title}</span>
          <Badge tone={card.statusTone as BadgeTone}>{card.status}</Badge>
        </span>
      </div>
      <div className="flex items-center justify-between gap-2 px-3 py-2 text-2xs text-text-muted">
        <span className="truncate">{dateText}</span>
        {card.source && <span className="shrink-0">via {card.source}</span>}
      </div>
    </div>
  )
}

/** A compact, deterministic day label for an RFC3339 instant ('' when empty). */
export function formatDay(iso: string): string {
  if (!iso) return ''
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  return new Date(t).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}
