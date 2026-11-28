// #151: the genre / studio facet tile (the reference genre-row chip: a
// pill with the facet name and an item count).
//
// Tiles are inert (divs) when there is no safe deep link — #150's list
// filters accept only a multi-value genre list, so a single studio's
// movies can't be queried server-side, and inventing a route is off-limits.
// The genre row keeps its tiles inert as well for the same reason; the
// tiles document what the library holds.
import { Link } from 'react-router-dom'
import { Badge } from '../primitives/badge'

export function FacetTile({
  name,
  count,
  target = null,
  testId,
}: {
  name: string
  count: number
  target?: string | null
  testId?: string
}) {
  const inner = (
    <>
      <span className="truncate text-sm font-semibold text-text">{name}</span>
      <Badge tone="neutral">{count}</Badge>
    </>
  )
  const cls =
    'discover-facet-tile flex w-44 shrink-0 items-center justify-between gap-2 rounded-lg border border-border bg-surface px-3 py-2' +
    (target != null ? ' transition-colors hover:border-accent/60' : '')
  if (target != null) {
    return (
      <Link to={target} className={cls} data-testid={testId}>
        {inner}
      </Link>
    )
  }
  return (
    <div className={cls} data-testid={testId} title={name}>
      {inner}
    </div>
  )
}
