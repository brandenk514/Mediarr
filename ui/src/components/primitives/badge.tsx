// Badge — design-system primitive (issue #115; duotone tones: issue #147).
//
// One badge component covers the badge families the unified UI needs:
// status badges (success / warning / error), kind badges (accent / info),
// genre / neutral chips (neutral), and the 14 Seerr duotone genre tones
// (red, darkred, blue, lightblue, darkblue, orange, darkorange, green,
// lightgreen, purple, darkpurple, yellow, black, pink — see
// ui/src/lib/tone-map.ts). Duotone tones render the reference pill style:
// dark-hex background, light-hex text/border. The legacy P1 badge in
// components/ui.tsx is untouched; the unified shell (#116) adopts this one.
import type { ReactNode } from 'react'
import { colorTones, type AvailableColors } from '../../lib/tone-map'

export type SemanticTone =
  | 'neutral'
  | 'accent'
  | 'info'
  | 'success'
  | 'warning'
  | 'error'

/** The six semantic tones plus the 14 Seerr duotone genre tones. */
export type BadgeTone = SemanticTone | AvailableColors

const BASE_CLASSES =
  'inline-flex items-center gap-1 whitespace-nowrap rounded-full border px-2 py-0.5 text-2xs font-semibold'

const SEMANTIC_TONE_CLASSES: Record<SemanticTone, string> = {
  neutral: 'border-border bg-surface-raised text-text-muted',
  accent: 'border-accent/30 bg-accent/15 text-accent',
  info: 'border-sky-500/30 bg-sky-500/15 text-sky-400',
  success: 'border-success/30 bg-success/15 text-success',
  warning: 'border-warning/30 bg-warning/15 text-warning',
  error: 'border-error/30 bg-error/15 text-error',
}

function isDuotoneTone(tone: BadgeTone): tone is AvailableColors {
  return Object.prototype.hasOwnProperty.call(colorTones, tone)
}

export interface BadgeProps {
  /** Visual tone: a semantic tone, or one of the 14 duotone genre tones.
   *  Defaults to neutral (genre / generic chips). */
  tone?: BadgeTone
  children: ReactNode
  /** Extra classes (e.g. sizing overrides). */
  className?: string
  testId?: string
}

export function Badge({ tone = 'neutral', children, className, testId }: BadgeProps) {
  // Duotone tones carry their color as an inline style (the hex pairs are
  // data, so they can't be static Tailwind classes); semantic tones keep
  // their token utilities.
  if (isDuotoneTone(tone)) {
    const [dark, light] = colorTones[tone]
    return (
      <span
        className={[BASE_CLASSES, className].filter(Boolean).join(' ')}
        style={{ backgroundColor: `#${dark}`, color: `#${light}`, borderColor: `#${light}` }}
        // No default testid: a page renders many badges, so a shared default
        // would make getByTestId('badge') throw on the second one.
        data-testid={testId}
      >
        {children}
      </span>
    )
  }
  return (
    <span
      className={[BASE_CLASSES, SEMANTIC_TONE_CLASSES[tone], className]
        .filter(Boolean)
        .join(' ')}
      // No default testid: a page renders many badges, so a shared default
      // would make getByTestId('badge') throw on the second one.
      data-testid={testId}
    >
      {children}
    </span>
  )
}

// BadgeGroup wraps a run of badges in a wrapping inline row (e.g. the genre
// line under a media title).
export function BadgeGroup({ children, testId }: { children: ReactNode; testId?: string }) {
  return <span className="inline-flex flex-wrap items-center gap-1.5" data-testid={testId}>{children}</span>
}
