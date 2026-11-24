// SidebarItem — design-system primitive (issue #115).
//
// One navigation row for the unified shell's sidebar (#116): optional icon,
// label, an active state (aria-current + rail-active fill), and a trailing
// badge slot (e.g. a queue count) that the page fills with a Badge.
//
// It is a plain button: the shell decides routing (the legacy rail in
// pages/Layout.tsx keeps its own markup until the shell lands).
import type { ReactNode } from 'react'

export interface SidebarItemProps {
  label: string
  icon?: ReactNode
  /** Marks the row as the current destination. */
  active?: boolean
  /** Trailing badge slot (count, status chip, ...). */
  badge?: ReactNode
  onClick?: () => void
  title?: string
  className?: string
  testId?: string
}

export function SidebarItem({
  label,
  icon,
  active = false,
  badge,
  onClick,
  title,
  className,
  testId,
}: SidebarItemProps) {
  const stateClasses = active
    ? 'bg-rail-active text-text'
    : 'text-text-muted hover:bg-rail-hover hover:text-text'
  return (
    <button
      type="button"
      aria-current={active ? 'page' : undefined}
      className={[
        'flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-accent',
        stateClasses,
        className,
      ]
        .filter(Boolean)
        .join(' ')}
      onClick={onClick}
      title={title ?? label}
      data-testid={testId}
    >
      {icon && (
        <span aria-hidden className="flex h-5 w-5 flex-none items-center justify-center">
          {icon}
        </span>
      )}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {badge != null && (
        <span className="flex flex-none items-center" data-testid={testId ? `${testId}-badge` : undefined}>
          {badge}
        </span>
      )}
    </button>
  )
}
