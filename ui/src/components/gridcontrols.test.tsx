import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useCallback, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import {
  FacetCheckboxGroup,
  FacetDateRange,
  FacetNumberRange,
  FacetSelect,
  FilterDrawer,
  SortCluster,
  usePersistentView,
} from './gridcontrols'
import { usePagedList } from './list'

// Issue #150: the Seerr-style control cluster shared by the four
// per-kind pages — sort field + explicit direction toggle, the filter
// slide-over drawer (NOT a funnel dropdown) with the pluralized active
// count + Clear action, per-kind view-toggle persistence, and the
// facet primitives the drawer bodies compose.

describe('FilterDrawer', () => {
  it('pluralizes the active-filter count in the header', () => {
    const { rerender } = render(
      <FilterDrawer open onClose={vi.fn()} onClear={vi.fn()} activeCount={0}>
        <p>facets</p>
      </FilterDrawer>,
    )
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('0 Active Filters')
    rerender(
      <FilterDrawer open onClose={vi.fn()} onClear={vi.fn()} activeCount={1}>
        <p>facets</p>
      </FilterDrawer>,
    )
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('1 Active Filter')
    rerender(
      <FilterDrawer open onClose={vi.fn()} onClear={vi.fn()} activeCount={2}>
        <p>facets</p>
      </FilterDrawer>,
    )
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('2 Active Filters')
  })

  it('renders the body facets and a working Clear Active Filters action', async () => {
    const onClear = vi.fn()
    render(
      <FilterDrawer open onClose={vi.fn()} onClear={onClear} activeCount={3}>
        <label>
          Genre
          <input data-testid="facet-body" />
        </label>
      </FilterDrawer>,
    )
    expect(screen.getByTestId('facet-body')).toBeInTheDocument()
    const clear = screen.getByTestId('filter-drawer-clear')
    expect(clear).not.toBeDisabled()
    const user = userEvent.setup()
    await user.click(clear)
    expect(onClear).toHaveBeenCalledTimes(1)
  })

  it('disables Clear when no filter is active', () => {
    render(
      <FilterDrawer open onClose={vi.fn()} onClear={vi.fn()} activeCount={0}>
        <p>facets</p>
      </FilterDrawer>,
    )
    expect(screen.getByTestId('filter-drawer-clear')).toBeDisabled()
  })

  it('renders nothing when closed', () => {
    render(
      <FilterDrawer open={false} onClose={vi.fn()} onClear={vi.fn()} activeCount={1}>
        <p>facets</p>
      </FilterDrawer>,
    )
    expect(screen.queryByTestId('filter-drawer')).toBeNull()
  })

  it('closes via the close button', async () => {
    const onClose = vi.fn()
    render(
      <FilterDrawer open onClose={onClose} onClear={vi.fn()} activeCount={0}>
        <p>facets</p>
      </FilterDrawer>,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('filter-drawer-close'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('SortCluster', () => {
  const fields = [
    { value: 'title', label: 'Title' },
    { value: 'release_date', label: 'Release Date' },
    { value: 'rating', label: 'TMDB Rating' },
  ]

  it('renders the current direction; flipping reports the new direction without changing the field', async () => {
    const onDir = vi.fn()
    render(
      <SortCluster field="release_date" fields={fields} dir="desc" onDir={onDir} onField={vi.fn()} testId="movies" />,
    )
    const toggle = screen.getByTestId('movies-sort-dir')
    expect(toggle).toHaveAttribute('aria-pressed', 'true') // desc pressed
    const user = userEvent.setup()
    await user.click(toggle)
    expect(onDir).toHaveBeenCalledWith('asc')
  })

  it('selecting a field keeps the current direction', async () => {
    const onField = vi.fn()
    render(
      <SortCluster field="title" fields={fields} dir="asc" onDir={vi.fn()} onField={onField} testId="movies" />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('movies-sort-btn'))
    await user.click(screen.getByTestId('movies-sort-opt-rating'))
    expect(onField).toHaveBeenCalledWith('rating')
  })
})

describe('usePersistentView', () => {
  function Probe({ kind }: { kind: string }) {
    const [view, setView] = usePersistentView(kind)
    return (
      <>
        <span data-testid="probe-view">{view}</span>
        <button type="button" onClick={() => setView(view === 'grid' ? 'list' : 'grid')}>
          flip
        </button>
      </>
    )
  }

  it('persists the view per kind and restores it across mounts', async () => {
    localStorage.clear()
    const { unmount } = render(<Probe kind="movies" />)
    expect(screen.getByTestId('probe-view')).toHaveTextContent('grid') // default
    const user = userEvent.setup()
    await user.click(screen.getByText('flip'))
    expect(screen.getByTestId('probe-view')).toHaveTextContent('list')
    expect(localStorage.getItem('mediarr.view.movies')).toBe('list')
    unmount()

    // A different kind is independent.
    render(<Probe kind="tv" />)
    expect(screen.getByTestId('probe-view')).toHaveTextContent('grid')
    expect(localStorage.getItem('mediarr.view.tv')).toBeNull()
  })

  it('ignores a corrupted stored value', () => {
    localStorage.clear()
    localStorage.setItem('mediarr.view.music', 'banana')
    render(<Probe kind="music" />)
    expect(screen.getByTestId('probe-view')).toHaveTextContent('grid')
  })
})

describe('facet primitives', () => {
  it('FacetSelect reports the chosen option', async () => {
    const onChange = vi.fn()
    render(
      <FacetSelect
        label="Status"
        value=""
        options={[
          { value: 'ended', label: 'Ended' },
          { value: 'continuing', label: 'Continuing' },
        ]}
        onChange={onChange}
        testId="facet-status"
      />,
    )
    const user = userEvent.setup()
    await user.selectOptions(screen.getByTestId('facet-status'), 'ended')
    expect(onChange).toHaveBeenCalledWith('ended')
  })

  it('FacetNumberRange reports min and max', async () => {
    function Harness() {
      const [min, setMin] = useState<number | null>(null)
      const [max, setMax] = useState<number | null>(null)
      return (
        <>
          <FacetNumberRange
            label="Runtime"
            min={min}
            max={max}
            onMin={setMin}
            onMax={setMax}
            testId="facet-runtime"
          />
          <span data-testid="runtime-out">
            {min ?? '—'}/{max ?? '—'}
          </span>
        </>
      )
    }
    render(<Harness />)
    const user = userEvent.setup()
    await user.type(screen.getByTestId('facet-runtime-min'), '90')
    await user.type(screen.getByTestId('facet-runtime-max'), '180')
    expect(screen.getByTestId('runtime-out')).toHaveTextContent('90/180')
  })

  it('FacetDateRange reports from and to', async () => {
    function Harness() {
      const [from, setFrom] = useState('')
      const [to, setTo] = useState('')
      return (
        <>
          <FacetDateRange label="Release" from={from} to={to} onFrom={setFrom} onTo={setTo} testId="facet-date" />
          <span data-testid="date-out">
            {from || '—'}/{to || '—'}
          </span>
        </>
      )
    }
    render(<Harness />)
    const user = userEvent.setup()
    await user.type(screen.getByTestId('facet-date-from'), '2020-01-01')
    await user.type(screen.getByTestId('facet-date-to'), '2023-12-31')
    expect(screen.getByTestId('date-out')).toHaveTextContent('2020-01-01/2023-12-31')
  })

  it('FacetCheckboxGroup toggles values and reports the full set', async () => {
    const onToggle = vi.fn()
    render(
      <FacetCheckboxGroup
        label="Genres"
        options={[
          { value: 'Action', label: 'Action' },
          { value: 'Drama', label: 'Drama' },
        ]}
        values={['Action']}
        onToggle={onToggle}
        testId="facet-genres"
      />,
    )
    expect(screen.getByTestId('facet-genres-opt-Action')).toBeChecked()
    const user = userEvent.setup()
    await user.click(screen.getByTestId('facet-genres-opt-Drama'))
    expect(onToggle).toHaveBeenCalledWith(['Action', 'Drama'])
    await user.click(screen.getByTestId('facet-genres-opt-Action'))
    expect(onToggle).toHaveBeenLastCalledWith([])
  })
})

// A load fn the append-mode hook can follow. P is structural: the hook
// only reads items/total/page.
function loadPages() {
  const pages = [
    { items: [{ id: 1 }, { id: 2 }], total: 5, page: 1, page_size: 2 },
    { items: [{ id: 3 }, { id: 4 }], total: 5, page: 2, page_size: 2 },
    { items: [{ id: 5 }], total: 5, page: 3, page_size: 2 },
  ]
  return (query: { page: number; q?: string }) =>
    Promise.resolve(pages[query.page - 1] ?? { items: [], total: 0, page: query.page, page_size: 2 })
}

describe('usePagedList append mode (infinite scroll)', () => {
  interface TestPage {
    items: { id: number }[]
    total: number
    page: number
  }

  function Grid() {
    // useCallback keeps the load identity stable across renders so the
    // hook's effect does not re-fire mid-accumulation.
    const load = useCallback(loadPages() as unknown as (q: import('../api/types').ListQuery) => Promise<TestPage>, [])
    const { items, total, hasMore, loadMore } = usePagedList<TestPage>(load, { mode: 'append' })
    return (
      <>
        <span data-testid="count">
          {items.length}/{total}
        </span>
        <button type="button" disabled={!hasMore} onClick={loadMore} data-testid="more">
          more
        </button>
      </>
    )
  }

  it('accumulates pages as loadMore fires and stops at the end', async () => {
    render(<Grid />)
    expect(await screen.findByText('2/5')).toBeInTheDocument()
    const user = userEvent.setup()
    await user.click(screen.getByTestId('more'))
    expect(await screen.findByText('4/5')).toBeInTheDocument()
    await user.click(screen.getByTestId('more'))
    expect(await screen.findByText('5/5')).toBeInTheDocument()
    expect(screen.getByTestId('more')).toBeDisabled()
  })

  it('a sort change deep in the accumulation restarts from page 1', async () => {
    // Two orderings over the same 5 items. Flipping the sort mid-scroll
    // (a new load identity) must restart the grid from page 1 of the new
    // order — not continue from a stale cursor, which would show a hole
    // in the list and drop items the user already scrolled past.
    let sort: 'asc' | 'desc' = 'asc'
    const order = () => (sort === 'desc' ? [5, 4, 3, 2, 1] : [1, 2, 3, 4, 5])

    function SortFlipGrid() {
      const [version, setVersion] = useState(0)
      const load = useCallback(
        (query: { page?: number }) => {
          const base = order()
          const p = query.page ?? 1
          return Promise.resolve({
            items: base.slice((p - 1) * 2, p * 2).map((id) => ({ id })),
            total: 5,
            page: p,
          })
        },
        [version],
      )
      const { items, hasMore, loadMore } = usePagedList<TestPage>(load, { mode: 'append' })
      return (
        <>
          <span data-testid="ids">{items.map((i) => i.id).join(',')}</span>
          <button type="button" disabled={!hasMore} onClick={loadMore} data-testid="more">
            more
          </button>
          <button
            type="button"
            data-testid="flip"
            onClick={() => {
              sort = sort === 'asc' ? 'desc' : 'asc'
              setVersion((v) => v + 1)
            }}
          >
            flip
          </button>
        </>
      )
    }

    render(<SortFlipGrid />)
    expect(await screen.findByTestId('ids')).toHaveTextContent('1,2')
    const user = userEvent.setup()
    await user.click(screen.getByTestId('more'))
    expect(await screen.findByTestId('ids')).toHaveTextContent('1,2,3,4')

    // Flip the sort deep in the accumulation.
    await user.click(screen.getByTestId('flip'))
    // The grid restarts from page 1 of the new order.
    expect(await screen.findByTestId('ids')).toHaveTextContent('5,4')

    // ...and can keep appending the rest of the new order.
    await user.click(screen.getByTestId('more'))
    expect(await screen.findByTestId('ids')).toHaveTextContent('5,4,3,2')
    await user.click(screen.getByTestId('more'))
    expect(await screen.findByTestId('ids')).toHaveTextContent('5,4,3,2,1')
    expect(screen.getByTestId('more')).toBeDisabled()
  })
})
