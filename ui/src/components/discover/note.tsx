// #151: the styled grey-out degradation notes (the restyled #118 pattern).
//
// A fully-keyless feed row shows its friendly notes IN the row (muted,
// grey-out styling) instead of an EmptyState; a partial row shows a note
// where the missing cells would be. The text is always the one
// deterministic mapper's output (model.cellNote / model.rowNotes) — the
// raw wire reason never reaches the DOM.
import type { ReactNode } from 'react'

/** One muted grey-out note. */
export function DegradationNote({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <p className="discover-row-note my-1 w-36 shrink-0 rounded-lg border border-border/60 bg-surface-raised/40 px-3 py-2 text-xs leading-relaxed text-text-muted" data-testid={testId}>
      {children}
    </p>
  )
}

/**
 * The row's deduped friendly notes, stacked where the missing cells would
 * be (the #118 per-cell grey-out, restyled). Renders nothing for [].
 */
export function RowNotes({ notes }: { notes: string[] }) {
  if (notes.length === 0) return null
  return (
    <span className="flex shrink-0 flex-wrap items-start gap-2" data-testid="discover-row-notes">
      {notes.map((n, i) => (
        <DegradationNote key={i}>{n}</DegradationNote>
      ))}
    </span>
  )
}
