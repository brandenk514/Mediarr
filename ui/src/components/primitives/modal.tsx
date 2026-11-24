// Modal — design-system primitive (issue #115).
//
// Detail overlays and confirm dialogs for the unified shell (#116) and
// every view built on it. Renders into document.body through a portal so
// the overlay is independent of any scroll container or transformed
// ancestor in the page tree. Behavior contract:
//   * Escape closes, overlay click closes, panel click does not;
//   * focus moves into the dialog on open and is restored on close;
//   * role="dialog" + aria-modal + aria-labelledby for screen readers.
//
// The legacy P1 modal in components/ui.tsx is untouched.
import { useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'
import type { ReactNode } from 'react'

export interface ModalProps {
  title: string
  onClose: () => void
  children: ReactNode
  /** Optional action row rendered in the dialog footer. */
  footer?: ReactNode
  /** max-width of the dialog. sm ~ 384px, md ~ 512px, lg ~ 672px. */
  size?: 'sm' | 'md' | 'lg'
  testId?: string
}

const SIZE_CLASSES: Record<NonNullable<ModalProps['size']>, string> = {
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
}

export function Modal({ title, onClose, children, footer, size = 'md', testId }: ModalProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

  // Focus the dialog on open; restore focus to the trigger on close.
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    panelRef.current?.focus()
    return () => previouslyFocused?.focus()
  }, [])

  // Escape closes the dialog (document-level so it works from any focus).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      data-testid={testId ? `${testId}-overlay` : undefined}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`flex max-h-[85vh] w-full ${SIZE_CLASSES[size]} flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-2xl outline-none`}
        data-testid={testId}
      >
        <div className="flex items-center justify-between gap-4 border-b border-border px-4 py-3">
          <h3 id={titleId} className="truncate text-lg font-semibold text-text">
            {title}
          </h3>
          <button
            type="button"
            className="rounded p-1 text-text-muted transition-colors hover:bg-surface-raised hover:text-text focus-visible:ring-2 focus-visible:ring-accent"
            aria-label="Close"
            onClick={onClose}
            data-testid={testId ? `${testId}-close` : 'modal-close'}
          >
            <span className="block h-4 w-4" aria-hidden>
              ✕
            </span>
          </button>
        </div>
        <div className="overflow-y-auto px-4 py-3 text-sm text-text">{children}</div>
        {footer && <div className="flex items-center justify-end gap-2 border-t border-border px-4 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}

// ---- confirm dialog --------------------------------------------------------

export interface ConfirmDialogProps {
  title: string
  message: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** danger renders the confirm button in the error tone (destructive ops). */
  tone?: 'primary' | 'danger'
  /** While busy the confirm button is disabled (e.g. awaiting the API). */
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
  testId?: string
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  tone = 'primary',
  busy = false,
  onConfirm,
  onCancel,
  testId,
}: ConfirmDialogProps) {
  const confirmClasses =
    tone === 'danger'
      ? 'bg-error text-white hover:brightness-110 disabled:opacity-50'
      : 'bg-accent text-white hover:bg-accent-hover disabled:opacity-50'
  return (
    <Modal
      title={title}
      onClose={onCancel}
      size="sm"
      testId={testId}
      footer={
        <>
          <button
            type="button"
            className="rounded-md border border-border bg-transparent px-3 py-1.5 text-sm font-medium text-text-muted transition-colors hover:bg-surface-raised hover:text-text focus-visible:ring-2 focus-visible:ring-accent"
            onClick={onCancel}
            data-testid={testId ? `${testId}-cancel` : 'confirm-cancel'}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`rounded-md px-3 py-1.5 text-sm font-semibold transition-colors focus-visible:ring-2 focus-visible:ring-accent ${confirmClasses}`}
            onClick={onConfirm}
            disabled={busy}
            data-testid={testId ? `${testId}-confirm` : 'confirm-confirm'}
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </>
      }
    >
      {message}
    </Modal>
  )
}
