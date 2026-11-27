import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { Badge } from './badge'
import { SidebarItem } from './sidebar-item'

describe('SidebarItem', () => {
  it('renders its icon slot, label, and badge slot', () => {
    render(
      <SidebarItem
        testId="nav"
        icon="▦"
        label="Library"
        badge={<Badge testId="count">12</Badge>}
        onClick={vi.fn()}
      />,
    )
    const row = screen.getByTestId('nav')
    expect(row).toHaveTextContent('Library')
    expect(row).toHaveTextContent('▦')
    expect(screen.getByTestId('nav-badge')).toContainElement(screen.getByTestId('count'))
    expect(screen.getByText('12')).toBeInTheDocument()
  })

  it('omits the badge slot when no badge is given', () => {
    render(<SidebarItem testId="nav" label="Library" onClick={vi.fn()} />)
    expect(screen.queryByTestId('nav-badge')).toBeNull()
  })

  it('calls onClick when clicked', async () => {
    const onClick = vi.fn()
    render(<SidebarItem testId="nav" label="Library" onClick={onClick} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('nav'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('marks the active row with aria-current and an accent pill, distinctly from the idle row', () => {
    render(
      <div>
        <SidebarItem testId="nav-active" label="Library" active onClick={vi.fn()} />
        <SidebarItem testId="nav-idle" label="Queue" onClick={vi.fn()} />
      </div>,
    )
    const active = screen.getByTestId('nav-active')
    const idle = screen.getByTestId('nav-idle')
    expect(active).toHaveAttribute('aria-current', 'page')
    // Active is an accent pill: solid accent fill + light text, so it reads
    // visibly brighter than the muted idle row (issue #147).
    expect(active).toHaveClass('bg-accent', 'text-white')
    expect(idle).not.toHaveAttribute('aria-current')
    // Idle stays muted: dimmed text/icon with a hover fill.
    expect(idle).toHaveClass('text-text-muted', 'hover:bg-rail-hover')
    // Distinct class sets: each state's signature classes are absent from
    // the other state, so active and idle can never render the same.
    const activeClasses = new Set(active.className.split(/\s+/))
    const idleClasses = new Set(idle.className.split(/\s+/))
    for (const c of ['bg-accent', 'text-white']) {
      expect(idleClasses.has(c)).toBe(false)
    }
    expect(activeClasses.has('text-text-muted')).toBe(false)
    expect(activeClasses.has('hover:bg-rail-hover')).toBe(false)
    const activeOnly = [...activeClasses].filter((c) => !idleClasses.has(c))
    const idleOnly = [...idleClasses].filter((c) => !activeClasses.has(c))
    expect(activeOnly.length).toBeGreaterThan(0)
    expect(idleOnly.length).toBeGreaterThan(0)
  })

  it('derives the accessible name from the label and uses it as the title', () => {
    render(<SidebarItem testId="nav" label="Queue" onClick={vi.fn()} />)
    const row = screen.getByTestId('nav')
    expect(row).not.toHaveAttribute('aria-label')
    expect(screen.getByRole('button', { name: 'Queue' })).toBe(row)
    expect(row).toHaveAttribute('title', 'Queue')
  })
})
