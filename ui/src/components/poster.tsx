// Shared poster-grid building blocks for the per-kind libraries (P7, Radarr R1).
//
// TV and Movies both render the 6-column poster grid: a 2:3 poster (with an
// optional status ribbon over the top-left corner) above a light two-line
// footer — 'Monitored' state + quality profile. Poster art is not served yet,
// so the poster is a two-word-initials stand-in.
//
// Radarr R1 (movies): the ribbon is a status indicator (green = downloaded,
// red = missing, orange = queued, grey = unreleased/unmonitored) and each
// card gains top-left hover controls (Refresh movie / Edit movie). TV passes
// no actions, so the hover layer simply isn't rendered there.
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
  // Radarr R1 status string (e.g. "downloaded:monitored", "missing:monitored",
  // "queued", "unreleased"). Absent for kinds without a status model (TV).
  status?: string
}

// RibbonTone maps onto the .ribbon.ok/.warn/.err/.muted CSS variants.
export type RibbonTone = 'ok' | 'warn' | 'err' | 'muted'

export interface RibbonInfo {
  text: string
  tone: RibbonTone
}

// ribbonFor derives the card's status ribbon from a movie's Radarr status
// string. Without a status (older backends / TV) it falls back to the legacy
// behaviour: an "Unmonitored" ribbon on unmonitored items, none otherwise.
export function ribbonFor(item: Pick<PosterItem, 'monitored' | 'status'>): RibbonInfo | null {
  switch (item.status) {
    case 'downloaded:monitored':
    case 'downloaded:unmonitored':
      return { text: 'Downloaded', tone: 'ok' }
    case 'queued':
      return { text: 'Queued', tone: 'warn' }
    case 'unreleased':
      return { text: 'Unreleased', tone: 'muted' }
    case 'missing:monitored':
      return { text: 'Missing', tone: 'err' }
    case 'missing:unmonitored':
      return { text: 'Unmonitored', tone: 'muted' }
    default:
      return item.monitored ? null : { text: 'Unmonitored', tone: 'muted' }
  }
}

// PosterCard is one grid cell. The card itself is a <button> (opens the
// detail page); the optional hover actions live in a sibling overlay so the
// markup stays valid (no interactive element nested in the card button).
// testIdPrefix keeps the per-kind testids stable ('series' for TV, 'movie'
// for Movies).
export function PosterCard({
  item,
  testIdPrefix,
  onOpen,
  onRefresh,
  onEdit,
}: {
  item: PosterItem
  testIdPrefix: string
  onOpen: () => void
  onRefresh?: () => void
  onEdit?: () => void
}) {
  const { id, name, year, monitored, quality, status } = item
  const ribbon = ribbonFor({ monitored, status })
  const hasActions = onRefresh !== undefined || onEdit !== undefined
  return (
    <div
      className="poster-cell"
      data-testid={`${testIdPrefix}-card-${id}`}
      title={`${name}${year ? ` (${year})` : ''}`}
    >
      <button
        type="button"
        className="poster-card"
        onClick={onOpen}
        data-poster-letter={firstLetter(name)}
      >
        {ribbon && (
          <span className={`ribbon ${ribbon.tone}`} data-testid={`ribbon-${id}`}>
            {ribbon.text}
          </span>
        )}
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
      {hasActions && (
        <span className="card-actions">
          {onRefresh && (
            <button
              type="button"
              className="icon-btn"
              title="Refresh movie"
              aria-label={`Refresh ${name}`}
              data-testid={`card-refresh-${id}`}
              onClick={onRefresh}
            >
              ⟳
            </button>
          )}
          {onEdit && (
            <button
              type="button"
              className="icon-btn"
              title="Edit movie"
              aria-label={`Edit ${name}`}
              data-testid={`card-edit-${id}`}
              onClick={onEdit}
            >
              ✎
            </button>
          )}
        </span>
      )}
    </div>
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
  onRefresh,
  onEdit,
}: {
  items: PosterItem[]
  testIdPrefix: string
  gridTestId: string
  azTestId?: string
  onOpen: (item: PosterItem) => void
  onRefresh?: (item: PosterItem) => void
  onEdit?: (item: PosterItem) => void
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
              onRefresh={onRefresh ? () => onRefresh(i) : undefined}
              onEdit={onEdit ? () => onEdit(i) : undefined}
            />
          ))}
        </div>
      </div>
      {items.length > 0 && <AzJump available={letters} onJump={jumpTo} testId={azTestId} />}
    </div>
  )
}
