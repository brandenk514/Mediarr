// Seerr-style per-kind control cluster (issue #150).
//
// The four library pages compose these building blocks in their page
// heading: an explicit sort-direction icon toggle next to a sort-field
// dropdown (the direction flips without changing the field), a filter
// button that opens a SLIDE-OVER drawer (not a funnel dropdown) whose
// header shows the pluralized active-filter count with a Clear action,
// the per-kind view-toggle persistence, and the facet primitives the
// drawer bodies are built from.
import { useCallback, useEffect, useRef, useState } from 'react'
import type { BadgeTone } from './primitives/badge'
import { Menu, type MenuOption, type ViewMode } from './ui'

// ---- badge tone mapping (shared by the four per-kind grid pages) ----

// toneFor maps the legacy poster ribbon tones onto the Badge tones (the
// #150 grids render the legacy ribbons as PosterCard status badges).
export function toneFor(tone: 'ok' | 'warn' | 'err' | 'muted'): BadgeTone {
  switch (tone) {
    case 'ok':
      return 'success'
    case 'warn':
      return 'warning'
    case 'err':
      return 'error'
    default:
      return 'neutral'
  }
}

// ---- per-kind view persistence ----

// usePersistentView is the grid/table view toggle, persisted per kind so
// the choice survives reloads ("grid" default — #150: the poster grid is
// the default view for all four kinds). A corrupted stored value falls
// back to the default instead of throwing.
export function usePersistentView(kind: string): [ViewMode, (v: ViewMode) => void] {
  const [view, setView] = useState<ViewMode>(() => {
    try {
      const stored = localStorage.getItem(`mediarr.view.${kind}`)
      return stored === 'grid' || stored === 'list' ? stored : 'grid'
    } catch {
      return 'grid'
    }
  })
  const set = useCallback(
    (v: ViewMode) => {
      setView(v)
      try {
        localStorage.setItem(`mediarr.view.${kind}`, v)
      } catch {
        // Private mode / storage disabled: the toggle still works in-session.
      }
    },
    [kind],
  )
  return [view, set]
}

// ---- sort cluster (field dropdown + explicit direction toggle) ----

// SortCluster is the heading's sort control pair: the direction icon
// button (↑ ascending / ↓ descending, aria-pressed when descending) and
// the field dropdown. Flipping the direction never changes the field —
// the two are independent controls (#150 acceptance).
export function SortCluster({
  field,
  fields,
  dir,
  onDir,
  onField,
  testId,
}: {
  field: string
  fields: MenuOption[]
  dir: 'asc' | 'desc'
  onDir: (dir: 'asc' | 'desc') => void
  onField: (field: string) => void
  testId?: string
}) {
  const asc = dir === 'asc'
  return (
    <>
      <button
        type="button"
        className="menu-btn"
        onClick={() => onDir(asc ? 'desc' : 'asc')}
        aria-pressed={!asc}
        title={asc ? 'Ascending — click for descending' : 'Descending — click for ascending'}
        aria-label={`Sort direction: ${asc ? 'ascending' : 'descending'}`}
        data-testid={`${testId ?? 'sort'}-sort-dir`}
      >
        <span aria-hidden>{asc ? '↑' : '↓'}</span>
      </button>
      <Menu
        label="Sort"
        options={fields}
        selected={field}
        onSelect={onField}
        testId={`${testId ?? 'sort'}-sort`}
      />
    </>
  )
}

// ---- the filter drawer ----

// FilterDrawer is the slide-over panel the filter button opens. The
// header follows the reference FilterSlideover pattern: "N Active
// Filter(s)" (pluralized) with a Clear Active Filters action (disabled
// at zero); the body is the page's facet controls, passed as children
// so each kind composes its own facet set.
export function FilterDrawer({
  open,
  onClose,
  onClear,
  activeCount,
  children,
  testId,
}: {
  open: boolean
  onClose: () => void
  onClear: () => void
  activeCount: number
  children?: React.ReactNode
  testId?: string
}) {
  // Escape closes the drawer (the mobile nav-drawer convention).
  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  const base = testId ?? 'filter'
  const plural = activeCount === 1 ? 'Filter' : 'Filters'
  return (
    <div className="filter-drawer-overlay" data-testid={`${base}-drawer-overlay`} onClick={onClose}>
      <aside
        className="filter-drawer"
        role="dialog"
        aria-label="Filters"
        data-testid="filter-drawer"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="filter-drawer-head">
          <span data-testid="filter-drawer-count">
            {activeCount} Active {plural}
          </span>
          <div className="filter-drawer-head-actions">
            <button
              type="button"
              className="ghost"
              onClick={onClear}
              disabled={activeCount === 0}
              data-testid="filter-drawer-clear"
            >
              Clear Active Filters
            </button>
            <button
              type="button"
              className="icon-btn"
              onClick={onClose}
              aria-label="Close filters"
              data-testid="filter-drawer-close"
            >
              ✕
            </button>
          </div>
        </header>
        <div className="filter-drawer-body">{children}</div>
      </aside>
    </div>
  )
}

// ---- infinite scroll sentinel (#150: no pagination chrome in grid mode) ----

// InfiniteSentinel is the invisible div after the grid. When it scrolls
// into view (and more pages exist) it fires onLoadMore; the intersection
// observer is skipped entirely where the API is unavailable (jsdom), so
// component tests drive loadMore through the hook directly.
export function InfiniteSentinel({
  onLoadMore,
  disabled,
  testId,
}: {
  onLoadMore: () => void
  disabled?: boolean
  testId?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (disabled) return
    if (typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) onLoadMore()
    })
    if (ref.current) io.observe(ref.current)
    return () => io.disconnect()
  }, [disabled, onLoadMore])
  return <div ref={ref} data-testid={testId ?? 'infinite-sentinel'} aria-hidden style={{ height: 1 }} />
}

// ---- facet primitives (the drawer bodies) ----

// FacetSelect is a labeled single-choice dropdown; "" = unset.
export function FacetSelect({
  label,
  value,
  options,
  onChange,
  testId,
}: {
  label: string
  value: string
  options: MenuOption[]
  onChange: (value: string) => void
  testId?: string
}) {
  return (
    <label className="facet">
      <span className="facet-label">{label}</span>
      <select value={value} onChange={(e) => onChange(e.target.value)} data-testid={testId}>
        <option value="">Any</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  )
}

// FacetText is a labeled free-text input; "" = unset.
export function FacetText({
  label,
  value,
  onChange,
  testId,
  placeholder,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  testId?: string
  placeholder?: string
}) {
  return (
    <label className="facet">
      <span className="facet-label">{label}</span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        data-testid={testId}
      />
    </label>
  )
}

// FacetNumberRange is a labeled min/max pair; null = open bound.
export function FacetNumberRange({
  label,
  min,
  max,
  onMin,
  onMax,
  testId,
}: {
  label: string
  min: number | null
  max: number | null
  onMin: (v: number | null) => void
  onMax: (v: number | null) => void
  testId?: string
}) {
  const parse = (s: string) => (s.trim() === '' ? null : Number(s))
  return (
    <div className="facet">
      <span className="facet-label">{label}</span>
      <div className="facet-range">
        <input
          type="number"
          min={0}
          value={min ?? ''}
          placeholder="min"
          onChange={(e) => onMin(e.target.value === '' ? null : parse(e.target.value))}
          data-testid={`${testId ?? 'facet'}-min`}
        />
        <input
          type="number"
          min={0}
          value={max ?? ''}
          placeholder="max"
          onChange={(e) => onMax(e.target.value === '' ? null : parse(e.target.value))}
          data-testid={`${testId ?? 'facet'}-max`}
        />
      </div>
    </div>
  )
}

// FacetDateRange is a labeled from/to date pair; "" = open bound.
export function FacetDateRange({
  label,
  from,
  to,
  onFrom,
  onTo,
  testId,
}: {
  label: string
  from: string
  to: string
  onFrom: (v: string) => void
  onTo: (v: string) => void
  testId?: string
}) {
  return (
    <div className="facet">
      <span className="facet-label">{label}</span>
      <div className="facet-range">
        <input
          type="date"
          value={from}
          onChange={(e) => onFrom(e.target.value)}
          data-testid={`${testId ?? 'facet'}-from`}
        />
        <input type="date" value={to} onChange={(e) => onTo(e.target.value)} data-testid={`${testId ?? 'facet'}-to`} />
      </div>
    </div>
  )
}

// FacetCheckboxGroup is a labeled multi-select checkbox set; the
// callback receives the full new set (order preserved).
export function FacetCheckboxGroup({
  label,
  options,
  values,
  onToggle,
  testId,
}: {
  label: string
  options: MenuOption[]
  values: string[]
  onToggle: (values: string[]) => void
  testId?: string
}) {
  const base = testId ?? 'facet'
  return (
    <fieldset className="facet">
      <legend className="facet-label">{label}</legend>
      <div className="facet-checks">
        {options.map((o) => (
          <label key={o.value} className="facet-check">
            <input
              type="checkbox"
              checked={values.includes(o.value)}
              onChange={() =>
                onToggle(values.includes(o.value) ? values.filter((v) => v !== o.value) : [...values, o.value])
              }
              data-testid={`${base}-opt-${o.value}`}
            />
            {o.label}
          </label>
        ))}
      </div>
    </fieldset>
  )
}
