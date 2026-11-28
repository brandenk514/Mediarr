// #151: the Discover row chrome — a title, chevron scrollers, and a
// horizontally scrollable track (the Seerr Slider, ported to the #147 token
// classes; no react-spring needed: native scroll + overflow detection).
//
// Row order and testids are the contract #158 and the e2e specs rely on:
// each row renders as <section data-testid="discover-row-<id>"> with a
// `discover-row-<id>-prev` / `-next` scroller pair.
import { useCallback, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { ChevronLeftIcon, ChevronRightIcon } from '@heroicons/react/24/outline'

const PAGE_PX = 320

export interface DiscoverRowProps {
  /** Row id (deterministic testid: discover-row-<id>). */
  id: string
  /** Row title (e.g. "Trending"). */
  label: string
  /** The scrollable content (cards, tiles, notes). */
  children: ReactNode
}

/**
 * A cinematic feed row: the title with chevron scrollers, and a horizontal
 * scroll track (horizontal scroll is the responsive mechanism at
 * 375/768px per #123 — nothing is clipped). The track is keyboard
 * scrollable (ArrowLeft/ArrowRight/Home/End) when focused.
 */
export function DiscoverRow({ id, label, children }: DiscoverRowProps) {
  const trackRef = useRef<HTMLDivElement>(null)
  const [range, setRange] = useState({ canLeft: false, canRight: false })

  const measure = useCallback(() => {
    const el = trackRef.current
    if (!el) return
    const canLeft = el.scrollLeft > 2
    const canRight = el.scrollLeft + el.clientWidth < el.scrollWidth - 2
    setRange((r) => (r.canLeft === canLeft && r.canRight === canRight ? r : { canLeft, canRight }))
  }, [])

  const scroll = useCallback(
    (dir: -1 | 1) => {
      const el = trackRef.current
      if (!el) return
      el.scrollBy({ left: dir * PAGE_PX, behavior: 'smooth' })
    },
    [],
  )

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const el = trackRef.current
    if (!el) return
    switch (e.key) {
      case 'ArrowLeft':
        e.preventDefault()
        el.scrollBy({ left: -PAGE_PX, behavior: 'smooth' })
        break
      case 'ArrowRight':
        e.preventDefault()
        el.scrollBy({ left: PAGE_PX, behavior: 'smooth' })
        break
      case 'Home':
        e.preventDefault()
        el.scrollTo({ left: 0, behavior: 'smooth' })
        break
      case 'End':
        e.preventDefault()
        el.scrollTo({ left: el.scrollWidth, behavior: 'smooth' })
        break
    }
  }

  return (
    <section className="discover-row discover-row-carousel" data-testid={`discover-row-${id}`}>
      <div className="discover-row-header">
        <h2 className="discover-row-title">{label}</h2>
        <span className="discover-row-scrollers">
          <button
            type="button"
            className="discover-row-chevron"
            aria-label={`Scroll ${label} left`}
            data-testid={`discover-row-${id}-prev`}
            disabled={!range.canLeft}
            onClick={() => scroll(-1)}
          >
            <ChevronLeftIcon className="h-5 w-5" />
          </button>
          <button
            type="button"
            className="discover-row-chevron"
            aria-label={`Scroll ${label} right`}
            data-testid={`discover-row-${id}-next`}
            disabled={!range.canRight}
            onClick={() => scroll(1)}
          >
            <ChevronRightIcon className="h-5 w-5" />
          </button>
        </span>
      </div>
      <div
        ref={trackRef}
        role="group"
        aria-label={label}
        className="discover-row-track"
        onScroll={measure}
        onKeyDown={onKeyDown}
        tabIndex={0}
      >
        {children}
      </div>
    </section>
  )
}
