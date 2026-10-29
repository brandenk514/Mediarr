// Shared Sonarr-parity UI components (P1 design system).
//
// These are the building blocks every page composes: the icon-over-label
// toolbar (with divider groups), grid/list view toggle, Sort/Filter dropdown
// menus, the badge set, a modal, the A–Z jump index, and a scaffold empty
// state. They are intentionally UI-only — data wiring happens in the pages.
import { useEffect, useRef, useState } from 'react'

// ---- toolbar (icon-over-label, divider groups) ----

export function Toolbar({ left, right }: { left?: React.ReactNode; right?: React.ReactNode }) {
  return (
    <div className="toolbar">
      {left && (
        <div className="toolbar-left tb-group">
          {left}
        </div>
      )}
      {right && (
        <div className="toolbar-right tb-group">
          {right}
        </div>
      )}
    </div>
  )
}

// TbButton is one icon-over-label toolbar control. `divider` puts a leading
// vertical rule before the button so groups read as Sonarr's do.
export function TbButton({
  icon,
  label,
  onClick,
  disabled,
  divider,
  testId,
  title,
}: {
  icon: string
  label: string
  onClick?: () => void
  disabled?: boolean
  divider?: boolean
  testId?: string
  title?: string
}) {
  return (
    <>
      {divider && <span className="tb-sep" aria-hidden />}
      <button
        type="button"
        className={`tb-btn ${disabled ? 'tb-disabled' : ''}`}
        onClick={disabled ? undefined : onClick}
        disabled={disabled}
        title={title ?? label}
        data-testid={testId}
      >
        <span className="tb-icon" aria-hidden>
          {icon}
        </span>
        <span>{label}</span>
      </button>
    </>
  )
}

// ---- view toggle (grid / list) ----

export type ViewMode = 'grid' | 'list'

export function ViewToggle({
  view,
  onChange,
  testId,
}: {
  view: ViewMode
  onChange: (v: ViewMode) => void
  testId?: string
}) {
  return (
    <div className="view-toggle" role="group" aria-label="View">
      <button
        type="button"
        className={view === 'grid' ? 'active' : ''}
        onClick={() => onChange('grid')}
        aria-pressed={view === 'grid'}
        data-testid={`${testId ?? 'view-toggle'}-grid`}
        title="Grid view"
      >
        ▦
      </button>
      <button
        type="button"
        className={view === 'list' ? 'active' : ''}
        onClick={() => onChange('list')}
        aria-pressed={view === 'list'}
        data-testid={`${testId ?? 'view-toggle'}-list`}
        title="List view"
      >
        ☰
      </button>
    </div>
  )
}

// ---- dropdown menu (Sort / Filter) ----

export interface MenuOption {
  value: string
  label: string
}

// Menu is a compact dropdown used for Sort / Filter. It closes on outside
// click or Escape. `title` is an optional section header inside the panel.
export function Menu({
  label,
  icon,
  options,
  selected,
  onSelect,
  title,
  testId,
  width,
}: {
  label: string
  icon?: string
  options: MenuOption[]
  selected: string
  onSelect: (value: string) => void
  title?: string
  testId?: string
  width?: number
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
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

  return (
    <div className="menu" ref={ref} data-testid={testId}>
      <button
        type="button"
        className="menu-btn"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid={`${testId ?? 'menu'}-btn`}
      >
        {icon && <span aria-hidden>{icon}</span>}
        {label}
        <span className="rail-caret" aria-hidden>
          ▾
        </span>
      </button>
      {open && (
        <div className="menu-panel" role="menu" style={width ? { minWidth: width } : undefined}>
          {title && <div className="menu-title">{title}</div>}
          {options.map((o) => (
            <button
              key={o.value}
              type="button"
              role="menuitem"
              className={o.value === selected ? 'sel' : ''}
              onClick={() => {
                onSelect(o.value)
                setOpen(false)
              }}
              data-testid={`${testId ?? 'menu'}-opt-${o.value}`}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ---- badges ----

export type BadgeTone = 'default' | 'teal' | 'ok' | 'warn' | 'err'

const TONE_CLASS: Record<BadgeTone, string> = {
  default: 'badge',
  teal: 'badge badge-teal',
  ok: 'badge badge-ok',
  warn: 'badge badge-warn',
  err: 'badge badge-err',
}

export function Badge({
  tone = 'default',
  children,
  testId,
}: {
  tone?: BadgeTone
  children: React.ReactNode
  testId?: string
}) {
  return (
    <span className={TONE_CLASS[tone]} data-testid={testId}>
      {children}
    </span>
  )
}

export function BadgeRow({ children }: { children: React.ReactNode }) {
  return <span className="badge-row">{children}</span>
}

// ---- settings section layout + unsaved-changes bar ----

// UnsavedBar is the sticky "you have unsaved changes" bar Sonarr shows at the
// bottom of settings while a form is dirty. `onSave` / `onDiscard` are wired
// by the page once per-section forms land (P5); in P1 it is the scaffold.
export function UnsavedBar({
  onSave,
  onDiscard,
  testId,
}: {
  onSave: () => void
  onDiscard: () => void
  testId?: string
}) {
  return (
    <div className="unsaved-bar" data-testid={testId ?? 'unsaved-bar'}>
      <span data-testid="unsaved-bar-text">You have unsaved changes.</span>
      <div>
        <button type="button" className="ghost" onClick={onDiscard} data-testid="unsaved-discard">
          Discard
        </button>
        <button type="button" onClick={onSave} data-testid="unsaved-save">
          Save
        </button>
      </div>
    </div>
  )
}

// SettingsLayout is the settings IA frame (P1 contract): a left section nav +
// a content pane, with an optional unsaved-changes bar. The per-section forms
// (P5) render into `children`.
export function SettingsLayout({
  nav,
  children,
  unsaved,
  onSave,
  onDiscard,
  testId,
}: {
  nav: React.ReactNode
  children: React.ReactNode
  unsaved?: boolean
  onSave?: () => void
  onDiscard?: () => void
  testId?: string
}) {
  return (
    <div className="settings-shell" data-testid={testId ?? 'settings-shell'}>
      <nav className="settings-nav" aria-label="Settings sections">
        {nav}
      </nav>
      <div style={{ minWidth: 0 }}>{children}</div>
      {unsaved && onSave && onDiscard && <UnsavedBar onSave={onSave} onDiscard={onDiscard} />}
    </div>
  )
}

// ---- modal ----

export function Modal({
  title,
  onClose,
  children,
  footer,
  testId,
  className,
}: {
  title: string
  onClose: () => void
  children: React.ReactNode
  footer?: React.ReactNode
  testId?: string
  className?: string
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-overlay" data-testid={testId ?? 'modal'} onMouseDown={onClose}>
      <div
        className={className ? `modal ${className}` : 'modal'}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="modal-head">
          <h3>{title}</h3>
          <button type="button" className="modal-x" onClick={onClose} aria-label="Close" data-testid="modal-close">
            ✕
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  )
}

// ---- A–Z jump index (right edge of library grid pages) ----

// AzJump renders a fixed column of A–Z letters. `available` lists the letters
// that have at least one item; those are clickable. `onJump` receives the
// letter so the page can scroll to the first item starting with it.
export function AzJump({
  available,
  onJump,
  testId,
}: {
  available: string[]
  onJump: (letter: string) => void
  testId?: string
}) {
  const set = new Set(available)
  const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')
  return (
    <nav className="az-jump" aria-label="Jump to letter" data-testid={testId ?? 'az-jump'}>
      {letters.map((l) => {
        const has = set.has(l)
        return (
          <button
            key={l}
            type="button"
            className={has ? 'has' : ''}
            disabled={!has}
            onClick={has ? () => onJump(l) : undefined}
            aria-label={has ? `Jump to ${l}` : undefined}
          >
            {l}
          </button>
        )
      })}
    </nav>
  )
}

// ---- empty state (scaffold placeholder) ----

export function EmptyState({
  title,
  message,
  children,
  testId,
}: {
  title: string
  message?: string
  children?: React.ReactNode
  testId?: string
}) {
  return (
    <div className="empty-state" data-testid={testId ?? 'empty-state'}>
      <h3>{title}</h3>
      {message && <p>{message}</p>}
      {children}
    </div>
  )
}
