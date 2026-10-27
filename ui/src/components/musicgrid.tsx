// Lidarr L1 artist grid: the 6-up square-cover card grid + A–Z quick jump.
//
// Card anatomy (per the Lidarr IA map): a square cover area (artist artwork
// isn't served yet, so a styled initials tile — no cover-art pipeline in
// L1) over a thin colored STATUS RIBBON strip, then a light footer panel
// with three centered lines: artist name (bold), Monitored/Unmonitored,
// quality profile. Unmonitored artists get the red corner fold (Lidarr's
// "flagged" corner), which is separate from the status ribbon strip.
import { firstLetter, posterText } from './poster'
import { FALLBACK_PROFILE_LABEL, deriveMusicRibbon } from './music'
import type { Artist } from '../api/types'
import { AzJump } from './ui'

export interface MusicGridItem {
  id: number
  name: string
  monitored: boolean
  status?: Artist['Status']
  qualityProfile?: string
  trackCount?: number
  fileCount?: number
  queuedCount?: number
}

// MusicCard is one grid cell. The card itself is a <button> (opens the
// artist detail); the red corner fold marks a flagged (unmonitored) artist.
export function MusicCard({
  item,
  onOpen,
}: {
  item: MusicGridItem
  onOpen: () => void
}) {
  const { id, name, monitored, status, qualityProfile } = item
  const ribbon = deriveMusicRibbon({
    Monitored: monitored,
    Status: status,
    TrackCount: item.trackCount,
    FileCount: item.fileCount,
    QueuedCount: item.queuedCount,
  })
  return (
    <div className="poster-cell" data-testid={`artist-card-${id}`} title={name}>
      <button
        type="button"
        className="poster-card"
        onClick={onOpen}
        data-poster-letter={firstLetter(name)}
        aria-label={name}
      >
        {!monitored && <span className="corner-flag" aria-hidden data-testid={`artist-flag-${id}`} />}
        <span className="music-cover" aria-hidden>
          {posterText(name)}
        </span>
        <span
          className={`music-ribbon ${ribbon.tone}`}
          data-testid={`ribbon-${id}`}
          data-ribbon-state={ribbon.state}
        >
          {ribbon.text}
        </span>
        <span className="music-foot">
          <span className="mf-name">{name}</span>
          <span className="mf-mon">{monitored ? 'Monitored' : 'Unmonitored'}</span>
          <span className="mf-q" data-testid={`card-quality-${id}`}>
            {qualityProfile || FALLBACK_PROFILE_LABEL}
          </span>
        </span>
      </button>
    </div>
  )
}

// MusicGrid renders the grid cells plus the right-edge A–Z quick-jump (the
// PageJumpBar role — only letters that have entries are clickable). The jump
// scrolls to the first card whose letter matches (via data-poster-letter).
export function MusicGrid({
  items,
  gridTestId,
  azTestId,
  onOpen,
}: {
  items: MusicGridItem[]
  gridTestId: string
  azTestId?: string
  onOpen: (item: MusicGridItem) => void
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
            <MusicCard key={i.id} item={i} onOpen={() => onOpen(i)} />
          ))}
        </div>
      </div>
      {items.length > 0 && <AzJump available={letters} onJump={jumpTo} testId={azTestId} />}
    </div>
  )
}

// fromArtist maps a wire Artist onto the grid item (the L1 aggregate fields
// are optional in the schema; absent counts read as 0 in the derivation).
export function fromArtist(a: Artist): MusicGridItem {
  return {
    id: a.ID,
    name: a.Name,
    monitored: a.Monitored,
    status: a.Status,
    qualityProfile: a.QualityProfile,
    trackCount: a.TrackCount,
    fileCount: a.FileCount,
    queuedCount: a.QueuedCount,
  }
}
