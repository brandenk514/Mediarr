// PosterCard — design-system primitive (issue #115).
//
// One grid cell for poster/cover art: the art (or an initials stand-in when
// no art is served yet), title, a meta line (year · runtime, season counts,
// ...), an optional status badge over the art, and a hover overlay for
// actions (refresh / edit / manage). The unified shell's library grids
// (#117–#123) compose these.
//
// Markup keeps the card a single <button> (opens detail) with the hover
// actions in a sibling overlay — no interactive element nested in the card
// button — the same pattern the legacy components/poster.tsx uses.
//
// The legacy P1 PosterCard in components/poster.tsx is untouched.
import type { ReactNode } from 'react'
import { Badge } from './badge'
import type { BadgeTone } from './badge'

export interface PosterCardProps {
  title: string
  /** Poster / cover art URL. Omit for the initials stand-in. */
  image?: string | null
  /** Second line under the title (e.g. "2021 · 166m"). */
  meta?: string
  /** Status badge rendered over the top-left of the art. */
  status?: { label: string; tone: BadgeTone }
  onOpen?: () => void
  /** Actions shown over the art on hover / focus. */
  hoverActions?: ReactNode
  className?: string
  testId?: string
}

// posterInitials derives the stand-in glyph from the title: first letters of
// the first two words, skipping leading articles ("The Wire" -> "W").
export function posterInitials(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  const letters = words.slice(0, 2).map((w) => w[0]?.toUpperCase() ?? '')
  return letters.join('') || '?'
}

export function PosterCard({
  title,
  image,
  meta,
  status,
  onOpen,
  hoverActions,
  className,
  testId,
}: PosterCardProps) {
  return (
    <div
      className={`group relative ${className ?? ''}`}
      data-testid={testId}
      title={title}
    >
      <button
        type="button"
        onClick={onOpen}
        className="block w-full overflow-hidden rounded-lg border border-border bg-surface text-left transition-colors hover:border-accent/60 focus-visible:ring-2 focus-visible:ring-accent"
        data-testid={testId ? `${testId}-open` : undefined}
      >
        <span className="relative block aspect-[2/3] w-full overflow-hidden bg-surface-raised">
          {image ? (
            <img src={image} alt="" className="h-full w-full object-cover" />
          ) : (
            <span
              aria-hidden
              className="flex h-full w-full items-center justify-center text-xl font-bold tracking-widest text-text-muted"
            >
              {posterInitials(title)}
            </span>
          )}
          {status && (
            <span className="absolute left-1.5 top-1.5">
              <Badge tone={status.tone}>{status.label}</Badge>
            </span>
          )}
        </span>
        <span className="block border-t border-border px-2 py-1.5">
          <span className="block truncate text-sm font-semibold text-text">{title}</span>
          {meta && <span className="block truncate text-2xs text-text-muted">{meta}</span>}
        </span>
      </button>
      {hoverActions && (
        <span
          className="pointer-events-none absolute right-1.5 top-1.5 flex gap-1 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:opacity-100"
          data-testid={testId ? `${testId}-actions` : undefined}
        >
          {hoverActions}
        </span>
      )}
    </div>
  )
}
