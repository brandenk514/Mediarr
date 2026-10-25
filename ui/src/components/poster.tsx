// Shared poster-grid building blocks for the per-kind libraries (P7).
//
// TV and Movies both render the Sonarr-style 6-column poster grid: a 2:3
// poster (with an optional red status ribbon for unmonitored items) over a
// light two-line footer — 'Monitored' state + quality profile. Poster art is
// not served yet, so the poster is a two-word-initials stand-in. TV (P2) and
// Movies (P7) share these helpers instead of copy-pasting the grid.
import { AzJump } from './ui'

export function posterText(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '🎬'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}

export function firstLetter(title: string): string {
  // Skip leading articles ("The Wire" -> W, Sonarr-style).
  const words = title.trim().split(/\s+/)
  for (const w of words) {
    if (!/^(the|a|an)$/i.test(w) && /^[a-z]/i.test(w)) return w[0]!.toUpperCase()
  }
  const c = title.trim()[0]
  return c ? c.toUpperCase() : '#'
}

export interface PosterItem {
  id: number
  name: string
  year?: number
  monitored: boolean
  quality?: string
}

// PosterCard is one grid cell. testIdPrefix keeps the per-kind testids
// stable ('series' for TV, 'movie' for Movies). Clicking the card calls
// onOpen (the page navigates to the detail route).
export function PosterCard({
  item,
  testIdPrefix,
  onOpen,
}: {
  item: PosterItem
  testIdPrefix: string
  onOpen: () => void
}) {
  const { id, name, year, monitored, quality } = item
  return (
    <button
      type="button"
      className="poster-card"
      onClick={onOpen}
      data-testid={`${testIdPrefix}-card-${id}`}
      data-poster-letter={firstLetter(name)}
      title={`${name}${year ? ` (${year})` : ''}`}
    >
      {!monitored && <span className="ribbon" data-testid={`ribbon-${id}`}>Unmonitored</span>}
      <span className="poster" aria-hidden>
        {posterText(name)}
      </span>
      <span className="poster-foot">
        <span className="pf-mon">{monitored ? 'Monitored' : 'Unmonitored'}</span>
        <span className="pf-q" data-testid={`card-quality-${id}`}>
          {quality || '—'}
        </span>
      </span>
    </button>
  )
}

// PosterGrid renders the grid cells plus the right-edge A–Z quick-jump. The
// jump scrolls to the first card whose letter matches (via data-poster-letter).
export function PosterGrid({
  items,
  testIdPrefix,
  gridTestId,
  azTestId,
  onOpen,
}: {
  items: PosterItem[]
  testIdPrefix: string
  gridTestId: string
  azTestId?: string
  onOpen: (item: PosterItem) => void
}) {
  const letters = [...new Set(items.map((i) => firstLetter(i.name)))].sort()

  function jumpTo(letter: string) {
    const el = document.querySelector(`[data-poster-letter="${letter}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }

  return (
    <div style={{ display: 'flex', gap: '0.5rem' }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="poster-grid" data-testid={gridTestId}>
          {items.map((i) => (
            <PosterCard
              key={i.id}
              item={i}
              testIdPrefix={testIdPrefix}
              onOpen={() => onOpen(i)}
            />
          ))}
        </div>
      </div>
      {items.length > 0 && <AzJump available={letters} onJump={jumpTo} testId={azTestId} />}
    </div>
  )
}
