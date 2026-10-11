import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ListControls, MonitoredToggle, Pagination, type MonitoredFilter } from './list'

// Component tests for the shared #41 list primitives: the optimistic
// MonitoredToggle (with rollback on API failure), the Pagination control, and
// the ListControls search/filter bar.
describe('MonitoredToggle', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the monitored state as a pressed on/off button', () => {
    render(<MonitoredToggle monitored={true} onToggle={vi.fn().mockResolvedValue(undefined)} />)
    const btn = screen.getByTestId('monitored-on')
    expect(btn).toHaveTextContent('monitored')
    expect(btn).toHaveAttribute('aria-pressed', 'true')
  })

  it('renders the unmonitored state as an unpressed on/off button', () => {
    render(<MonitoredToggle monitored={false} onToggle={vi.fn().mockResolvedValue(undefined)} />)
    const btn = screen.getByTestId('monitored-off')
    expect(btn).toHaveTextContent('off')
    expect(btn).toHaveAttribute('aria-pressed', 'false')
  })

  it('optimistically flips, then rolls back when the API rejects', async () => {
    // A deferred promise keeps the optimistic state stable so we can observe
    // the "off" flip, then reject it to exercise the rollback path.
    let rejectFn!: (e: Error) => void
    const pending = new Promise<never>((_, rej) => {
      rejectFn = rej
    })
    const onToggle = vi.fn(() => pending)
    render(<MonitoredToggle monitored={true} onToggle={onToggle} />)

    const user = userEvent.setup()
    await user.click(screen.getByTestId('monitored-on'))

    // Optimistic: onToggle is called with the NEW (opposite) value and the
    // button flips to "off" while the request is in flight.
    expect(onToggle).toHaveBeenCalledWith(false)
    expect(screen.getByText('off')).toBeInTheDocument()

    // Now reject: the UI rolls back to the server-truth prop value ("monitored").
    await act(async () => {
      rejectFn(new Error('boom'))
    })
    await waitFor(() => {
      expect(screen.getByText('monitored')).toBeInTheDocument()
    })
    expect(screen.getByTestId('monitored-on')).toBeInTheDocument()
  })

  it('stays flipped when the API accepts', async () => {
    const onToggle = vi.fn(() => Promise.resolve())
    render(<MonitoredToggle monitored={false} onToggle={onToggle} />)

    const user = userEvent.setup()
    await user.click(screen.getByTestId('monitored-off'))
    expect(onToggle).toHaveBeenCalledWith(true)
    await waitFor(() => {
      expect(screen.getByText('monitored')).toBeInTheDocument()
    })
  })
})

describe('Pagination', () => {
  it('renders nothing for a single page', () => {
    const { container } = render(<Pagination page={1} pageSize={25} total={10} onPage={vi.fn()} />)
    expect(container.firstChild).toBeNull()
  })

  it('shows page position and steps forward from the middle', () => {
    const onPage = vi.fn()
    render(<Pagination page={2} pageSize={10} total={25} onPage={onPage} />)
    expect(screen.getByTestId('pagination')).toHaveTextContent('page 2 of 3')
    expect(screen.getByTestId('page-prev')).toBeEnabled()
    expect(screen.getByTestId('page-next')).toBeEnabled()
    screen.getByTestId('page-next').click()
    expect(onPage).toHaveBeenCalledWith(3)
  })

  it('disables prev on the first page', () => {
    render(<Pagination page={1} pageSize={10} total={20} onPage={vi.fn()} />)
    expect(screen.getByTestId('page-prev')).toBeDisabled()
    expect(screen.getByTestId('page-next')).toBeEnabled()
  })

  it('disables next on the last page', () => {
    render(<Pagination page={2} pageSize={10} total={20} onPage={vi.fn()} />)
    expect(screen.getByTestId('page-next')).toBeDisabled()
    expect(screen.getByTestId('page-prev')).toBeEnabled()
  })
})

// ControlsHarness mirrors how the real pages drive ListControls: the search
// text and monitored filter are owned by parent state and echoed back, so the
// controlled input accumulates typed characters. The passed-in spies are
// invoked *in addition to* the local state setters so we can assert on the
// exact values the user's input produced.
function ControlsHarness({
  onText,
  onMonitored,
}: {
  onText: (s: string) => void
  onMonitored: (m: MonitoredFilter) => void
}) {
  const [text, setText] = useState('')
  const [mon, setMon] = useState<MonitoredFilter>('all')
  return (
    <ListControls
      text={text}
      onText={(v) => {
        setText(v)
        onText(v)
      }}
      monitored={mon}
      onMonitored={(m) => {
        setMon(m)
        onMonitored(m)
      }}
      onRefresh={vi.fn()}
      total={3}
      busy={false}
    />
  )
}

describe('ListControls', () => {
  it('wires the search box and three-way monitored filter', async () => {
    const onText = vi.fn()
    const onMonitored = vi.fn()
    const user = userEvent.setup()
    render(<ControlsHarness onText={onText} onMonitored={onMonitored} />)

    await user.type(screen.getByTestId('list-search'), 'dune')
    expect(onText).toHaveBeenLastCalledWith('dune')

    await user.click(screen.getByTestId('filter-monitored'))
    expect(onMonitored).toHaveBeenCalledWith('monitored')

    expect(screen.getByTestId('list-controls')).toHaveTextContent('3 items')
  })
})
