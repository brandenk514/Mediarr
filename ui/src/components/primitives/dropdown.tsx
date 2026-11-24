// Dropdown — design-system primitive (issue #115).
//
// The one dropdown for filters, sort menus, and other option pickers in the
// unified shell (#116) and its views. Behavior contract:
//   * trigger shows the selected option's label (or placeholder);
//   * panel is a real menu (role="menu" / "menuitem"), keyboard reachable;
//   * selecting reports the value and closes;
//   * Escape and outside click close without selecting;
//   * disabled options are inert.
//
// The legacy P1 Menu in components/ui.tsx is untouched.
import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'

export interface DropdownOption {
  value: string
  label: string
  disabled?: boolean
}

export interface DropdownProps {
  /**
   * Text shown on the trigger when no option is selected. When an option IS
   * selected, the trigger shows that option's label instead.
   */
  label: string
  options: DropdownOption[]
  /** Value of the currently selected option (drives trigger text + check). */
  selected?: string
  onChange: (value: string) => void
  /** Optional glyph rendered before the trigger text. */
  icon?: ReactNode
  align?: 'left' | 'right'
  testId?: string
}

export function Dropdown({ label, options, selected, onChange, icon, align = 'left', testId }: DropdownProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const selectedOption = options.find((o) => o.value === selected)

  return (
    <div className="relative inline-block" ref={rootRef} data-testid={testId}>
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 py-1.5 text-sm font-medium text-text transition-colors hover:bg-surface-raised focus-visible:ring-2 focus-visible:ring-accent"
        onClick={() => setOpen((o) => !o)}
        // No default testid: a page can render several dropdowns, so a
        // shared default would make getByTestId('dropdown-btn') throw.
        data-testid={testId ? `${testId}-btn` : undefined}
      >
        {icon && (
          <span aria-hidden className="text-text-muted">
            {icon}
          </span>
        )}
        {selectedOption ? selectedOption.label : label}
        <span aria-hidden className="text-text-muted">
          ▾
        </span>
      </button>
      {open && (
        <div
          role="menu"
          aria-orientation="vertical"
          className={`absolute top-full z-40 mt-1.5 max-h-80 min-w-44 overflow-y-auto rounded-md border border-border bg-surface py-1 shadow-2xl ${
            align === 'right' ? 'right-0' : 'left-0'
          }`}
          data-testid={testId ? `${testId}-menu` : undefined}
        >
          {options.map((o) => {
            const isSel = o.value === selected
            return (
              <button
                key={o.value}
                type="button"
                role="menuitem"
                disabled={o.disabled}
                className={`flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-sm transition-colors disabled:cursor-default disabled:opacity-50 ${
                  isSel ? 'font-semibold text-accent' : 'text-text hover:bg-surface-raised'
                }`}
                onClick={() => {
                  onChange(o.value)
                  setOpen(false)
                }}
                data-testid={testId ? `${testId}-opt-${o.value}` : undefined}
              >
                <span className="truncate">{o.label}</span>
                {isSel && (
                  <span aria-hidden className="text-accent">
                    ✓
                  </span>
                )}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
