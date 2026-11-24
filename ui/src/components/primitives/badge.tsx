// Badge — design-system primitive (issue #115).
//
// One badge component covers the three badge families the unified UI needs:
// status badges (success / warning / error), kind badges (accent / info),
// and genre / neutral chips (neutral). The legacy P1 badge in
// components/ui.tsx is untouched; the unified shell (#116) adopts this one.
import type { ReactNode } from 'react'

export type BadgeTone = 'neutral' | 'accent' | 'info' | 'success' | 'warning' | 'error'

const TONE_CLASSES: Record<BadgeTone, string> = {
  neutral: 'border-border bg-surface-raised text-text-muted',
  accent: 'border-accent/30 bg-accent/15 text-accent',
  info: 'border-sky-500/30 bg-sky-500/15 text-sky-400',
  success: 'border-success/30 bg-success/15 text-success',
  warning: 'border-warning/30 bg-warning/15 text-warning',
  error: 'border-error/30 bg-error/15 text-error',
}

export interface BadgeProps {
  /** Visual tone. Defaults to neutral (genre / generic chips). */
  tone?: BadgeTone
  children: ReactNode
  /** Extra classes (e.g. sizing overrides). */
  className?: string
  testId?: string
}

export function Badge({ tone = 'neutral', children, className, testId }: BadgeProps) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1 whitespace-nowrap rounded-full border px-2 py-0.5 text-2xs font-semibold',
        TONE_CLASSES[tone],
        className,
      ]
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
