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

  it('marks the active row with aria-current and the rail-active classes', () => {
    render(
      <div>
        <SidebarItem testId="nav-active" label="Library" active onClick={vi.fn()} />
        <SidebarItem testId="nav-idle" label="Queue" onClick={vi.fn()} />
      </div>,
    )
    const active = screen.getByTestId('nav-active')
    const idle = screen.getByTestId('nav-idle')
    expect(active).toHaveAttribute('aria-current', 'page')
    expect(active).toHaveClass('bg-rail-active', 'text-text')
    expect(idle).not.toHaveAttribute('aria-current')
    expect(idle).toHaveClass('text-text-muted')
    expect(idle).not.toHaveClass('bg-rail-active')
  })

  it('derives the accessible name from the label and uses it as the title', () => {
    render(<SidebarItem testId="nav" label="Queue" onClick={vi.fn()} />)
    const row = screen.getByTestId('nav')
    expect(row).not.toHaveAttribute('aria-label')
    expect(screen.getByRole('button', { name: 'Queue' })).toBe(row)
    expect(row).toHaveAttribute('title', 'Queue')
  })
})
