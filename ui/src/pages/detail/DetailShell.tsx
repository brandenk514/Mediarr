// Unified media detail shell (issue #120, part A).
//
// One shared detail surface for all four existing detail routes
// (/movies/:id, /tv/:id, /music/:id, /books/:id): back link, common action
// row, status line, hero (poster/backdrop area + title + kind badge +
// metadata) and the kind-specific body sections. The four pages keep their
// data loading, error/loading states, testids and body sections; the shell
// owns the shared chrome. The action row is a reusable slot for later work
// (issue #120 part B integrates the per-kind add flows).
import type { ReactNode } from 'react'

export type DetailKind = 'movie' | 'tv' | 'music' | 'book'

export const KIND_LABEL: Record<DetailKind, string> = {
  movie: 'Movie',
  tv: 'TV',
  music: 'Artist',
  book: 'Author',
}

export interface DetailNavButton {
  testId: string
  icon: string
  label: string
  onClick: () => void
}

export interface DetailShellProps {
  kind: DetailKind
  pageTestId: string
  loading: boolean
  loadingText: string
  loadingTestId: string
  error: string | null
  errorTitle: string
  errorTestId: string
  errorTestIdMessage: string
  authLost?: boolean
  onRetry: () => void
  back: { to: string; label: string; testId: string }
  actionsLeft?: ReactNode
  actionsRight?: ReactNode
  status?: string | null
  statusTestId: string
  title: string
  heading?: ReactNode
  posterUrl?: string | null
  heroTestId: string
  nav?: { prev?: DetailNavButton; next?: DetailNavButton }
  titleExtras?: ReactNode
  meta?: ReactNode
  children: ReactNode
}

export function DetailShell(props: DetailShellProps) {
  // STUB: behavior pending (TDD red).
  return <div className="list-page" data-testid="detail-shell-stub">{props.children}</div>
}
