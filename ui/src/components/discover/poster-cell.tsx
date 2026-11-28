// #151: the portrait feed cell (the reference portrait card: poster art or
// an initials stand-in, a kind pill, title + one-line meta, a hover border).
//
// The cell is a Link when it has a navigation target (the row chrome keeps
// it inert-able: no invented routes for books / non-deep-linkable items).
// Art that fails to load degrades to the initials stand-in (acceptance:
// poster cells are real art / initials stand-in / grey-out notes).
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../primitives/badge'
import { posterInitials } from '../primitives/poster-card'
import type { DiscoverKind } from '../../pages/discover/model'

export const KIND_PILL: Record<DiscoverKind, { label: string; tone: BadgeTone }> = {
  movie: { label: 'MOVIE', tone: 'info' },
  tv: { label: 'SERIES', tone: 'accent' },
  music: { label: 'MUSIC', tone: 'warning' },
  book: { label: 'BOOK', tone: 'success' },
}

export interface PosterCellProps {
  kind: DiscoverKind
  title: string
  /** One-line meta under the title (year, genre, author, date). '' = none. */
  meta?: string
  /** Poster / cover art URL. Missing/failed art renders the initials stand-in. */
  posterUrl?: string | null
  /** Navigation target; null/undefined renders an inert cell (a div). */
  target?: string | null
  /** Optional chip over the art's bottom-right (e.g. a release date). */
  chip?: string
  testId?: string
}

export function PosterCell({ kind, title, meta = '', posterUrl = null, target = null, chip, testId }: PosterCellProps) {
  const [imgFailed, setImgFailed] = useState(false)
  const showImage = posterUrl != null && posterUrl !== '' && !imgFailed
  const pill = KIND_PILL[kind]

  return (
    <div className="discover-card group w-36 shrink-0" title={title}>
      {target != null ? (
        <Link
          to={target}
          data-testid={testId}
          className="block overflow-hidden rounded-lg border border-border bg-surface transition-colors hover:border-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <CellArt title={title} showImage={showImage} posterUrl={posterUrl} onImgError={() => setImgFailed(true)} pill={pill} chip={chip} />
          <CellText title={title} meta={meta} />
        </Link>
      ) : (
        <div className="block overflow-hidden rounded-lg border border-border bg-surface" data-testid={testId}>
          <CellArt title={title} showImage={showImage} posterUrl={posterUrl} onImgError={() => setImgFailed(true)} pill={pill} chip={chip} />
          <CellText title={title} meta={meta} />
        </div>
      )}
    </div>
  )
}

function CellArt({
  title,
  showImage,
  posterUrl,
  onImgError,
  pill,
  chip,
}: {
  title: string
  showImage: boolean
  posterUrl?: string | null
  onImgError: () => void
  pill: { label: string; tone: BadgeTone }
  chip?: string
}) {
  return (
    <span className="relative block aspect-[2/3] w-full overflow-hidden bg-surface-raised">
      {showImage && posterUrl ? (
        <img src={posterUrl} alt="" className="h-full w-full object-cover" onError={onImgError} loading="lazy" />
      ) : (
        <span
          aria-hidden
          className="flex h-full w-full items-center justify-center text-xl font-bold tracking-widest text-text-muted"
        >
          {posterInitials(title)}
        </span>
      )}
      <span className="absolute left-1.5 top-1.5">
        <Badge tone={pill.tone}>{pill.label}</Badge>
      </span>
      {chip && (
        <span className="absolute bottom-1.5 right-1.5 rounded-full border border-border bg-bg/80 px-2 py-0.5 text-2xs font-semibold text-text-muted">
          {chip}
        </span>
      )}
    </span>
  )
}

function CellText({ title, meta }: { title: string; meta: string }) {
  return (
    <span className="block border-t border-border px-2 py-1.5">
      <span className="block truncate text-sm font-semibold text-text">{title}</span>
      {meta && <span className="block truncate text-2xs text-text-muted">{meta}</span>}
    </span>
  )
}
