import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { PosterCard, posterInitials } from './poster-card'

describe('posterInitials', () => {
  it('uses the first letters of the first two words', () => {
    expect(posterInitials('Dune Part Two')).toBe('DP')
  })

  it('skips leading articles (Sonarr-style)', () => {
    expect(posterInitials('The Wire')).toBe('W')
    // Article is skipped; initials come from the first two remaining words.
    expect(posterInitials('A History of God')).toBe('HO')
  })

  it('falls back to a placeholder for empty titles', () => {
    expect(posterInitials('   ')).toBe('?')
  })
})

describe('PosterCard', () => {
  it('renders the initials stand-in, title, and meta when no image is given', () => {
    render(
      <PosterCard
        testId="card"
        title="The Wire"
        meta="2002 · 6 seasons"
        onOpen={vi.fn()}
      />,
    )
    const open = screen.getByTestId('card-open')
    expect(open).toHaveTextContent('W')
    expect(open).toHaveTextContent('The Wire')
    expect(open).toHaveTextContent('2002 · 6 seasons')
    expect(open.querySelector('img')).toBeNull()
  })

  it('renders the provided poster image and no stand-in', () => {
    render(
      <PosterCard
        testId="card"
        title="Dune"
        image="/api/v1/media/poster/42"
        onOpen={vi.fn()}
      />,
    )
    const img = screen.getByTestId('card-open').querySelector('img')
    expect(img).not.toBeNull()
    expect(img).toHaveAttribute('src', '/api/v1/media/poster/42')
  })

  it('renders the status badge over the art when given', () => {
    render(
      <PosterCard
        testId="card"
        title="Dune"
        status={{ label: 'Queued', tone: 'warning' }}
        onOpen={vi.fn()}
      />,
    )
    const badge = screen.getByText('Queued')
    expect(badge).toHaveClass('text-warning')
  })

  it('does not render a status badge when status is absent', () => {
    render(<PosterCard testId="card" title="Dune" onOpen={vi.fn()} />)
    expect(screen.queryByTestId('card-open')).not.toHaveTextContent('Queued')
  })

  it('calls onOpen when the card is clicked', async () => {
    const onOpen = vi.fn()
    render(<PosterCard testId="card" title="Dune" onOpen={onOpen} />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('card-open'))
    expect(onOpen).toHaveBeenCalledTimes(1)
  })

  it('does not call onOpen when no handler is given', async () => {
    render(<PosterCard testId="card" title="Dune" />)
    const user = userEvent.setup()
    await user.click(screen.getByTestId('card-open'))
    expect(screen.getByTestId('card')).toBeInTheDocument()
  })

  it('renders hover actions and calls their handlers', async () => {
    const onRefresh = vi.fn()
    const onEdit = vi.fn()
    render(
      <PosterCard
        testId="card"
        title="Dune"
        onOpen={vi.fn()}
        hoverActions={
          <>
            <button type="button" onClick={onRefresh} data-testid="refresh" title="Refresh">
              ⟳
            </button>
            <button type="button" onClick={onEdit} data-testid="edit" title="Edit">
              ✎
            </button>
          </>
        }
      />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('refresh'))
    await user.click(screen.getByTestId('edit'))
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(onEdit).toHaveBeenCalledTimes(1)
  })

  it('omits the hover-actions layer when no actions are given', () => {
    render(<PosterCard testId="card" title="Dune" onOpen={vi.fn()} />)
    expect(screen.queryByTestId('card-actions')).toBeNull()
  })
})
