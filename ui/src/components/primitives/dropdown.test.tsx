import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Dropdown, type DropdownOption } from './dropdown'

const OPTIONS: DropdownOption[] = [
  { value: 'newest', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'az', label: 'Title A–Z' },
]

// SortHarness mirrors how a page drives the dropdown: selection is owned by
// parent state and echoed back, so the controlled trigger shows the live
// selection. The spy observes the values the user's choices produced.
function SortHarness({ onChange }: { onChange: (v: string) => void }) {
  const [selected, setSelected] = useState('newest')
  return (
    <Dropdown
      testId="sort"
      label="Sort"
      icon="↕"
      options={OPTIONS}
      selected={selected}
      onChange={(v) => {
        setSelected(v)
        onChange(v)
      }}
    />
  )
}

describe('Dropdown', () => {
  it('is closed initially and shows the selected option on the trigger', () => {
    render(<SortHarness onChange={vi.fn()} />)
    const btn = screen.getByTestId('sort-btn')
    expect(btn).toHaveTextContent('Newest first')
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByTestId('sort-menu')).toBeNull()
  })

  it('opens the menu with all options on trigger click', async () => {
    render(<SortHarness onChange={vi.fn()} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('sort-btn'))
    expect(screen.getByTestId('sort-btn')).toHaveAttribute('aria-expanded', 'true')
    const menu = screen.getByTestId('sort-menu')
    expect(menu).toHaveAttribute('role', 'menu')
    expect(screen.getByRole('menuitem', { name: /Newest first/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Oldest first/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Title A–Z/ })).toBeInTheDocument()
  })

  it('selecting an option reports the value, updates the trigger, and closes', async () => {
    const onChange = vi.fn()
    render(<SortHarness onChange={onChange} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('sort-btn'))
    await user.click(screen.getByTestId('sort-opt-az'))
    expect(onChange).toHaveBeenCalledWith('az')
    expect(screen.getByTestId('sort-btn')).toHaveTextContent('Title A–Z')
    expect(screen.queryByTestId('sort-menu')).toBeNull()
  })

  it('marks the currently selected option with the check marker', async () => {
    render(<SortHarness onChange={vi.fn()} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('sort-btn'))
    expect(screen.getByRole('menuitem', { name: /Newest first/ })).toHaveTextContent('✓')
    expect(screen.getByRole('menuitem', { name: /Oldest first/ })).not.toHaveTextContent('✓')
  })

  it('closes on Escape without selecting', async () => {
    const onChange = vi.fn()
    render(<SortHarness onChange={onChange} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('sort-btn'))
    await user.keyboard('{Escape}')
    expect(screen.queryByTestId('sort-menu')).toBeNull()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('closes on outside click without selecting', async () => {
    const onChange = vi.fn()
    render(<SortHarness onChange={onChange} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('sort-btn'))
    expect(screen.getByTestId('sort-menu')).toBeInTheDocument()
    await user.click(document.body)
    expect(screen.queryByTestId('sort-menu')).toBeNull()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('shows the label as placeholder when nothing is selected', () => {
    render(
      <Dropdown testId="f" label="Filter: monitored" options={OPTIONS} selected={undefined} onChange={vi.fn()} />,
    )
    expect(screen.getByTestId('f-btn')).toHaveTextContent('Filter: monitored')
  })

  it('marks unavailable options as disabled, so only enabled ones select', async () => {
    const onChange = vi.fn()
    render(
      <Dropdown
        testId="d"
        label="Indexer"
        options={[
          { value: 'n1', label: 'N1' },
          { value: 'n2', label: 'N2 (broken)', disabled: true },
        ]}
        selected="n1"
        onChange={onChange}
      />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('d-btn'))
    // A disabled menuitem cannot be activated (the browser suppresses its
    // click); the enabled option remains the working path.
    expect(screen.getByRole('menuitem', { name: /N2/ })).toBeDisabled()
    await user.click(screen.getByRole('menuitem', { name: 'N1' }))
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('n1')
  })
})
