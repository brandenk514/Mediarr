import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConfirmDialog, Modal } from './modal'

describe('Modal', () => {
  it('renders title and body through a portal on document.body', () => {
    const { container } = render(
      <Modal title="Movie details" onClose={vi.fn()} testId="m">
        <p>body-content</p>
      </Modal>,
    )
    const dialog = screen.getByRole('dialog', { name: 'Movie details' })
    // Portal proof: the dialog lives in document.body, not in the render
    // container the page mounted into.
    expect(document.body).toContainElement(dialog)
    expect(container.contains(dialog)).toBe(false)
    expect(screen.getByText('body-content')).toBeInTheDocument()
    expect(dialog).toHaveAttribute('aria-modal', 'true')
  })

  it('calls onClose from the close button', async () => {
    const onClose = vi.fn()
    render(
      <Modal title="t" onClose={onClose} testId="m">
        body
      </Modal>,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('m-close'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on Escape', async () => {
    const onClose = vi.fn()
    render(
      <Modal title="t" onClose={onClose} testId="m">
        body
      </Modal>,
    )
    const user = userEvent.setup()
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on overlay mousedown but not on panel mousedown', () => {
    const onClose = vi.fn()
    render(
      <Modal title="t" onClose={onClose} testId="m">
        body
      </Modal>,
    )
    const overlay = screen.getByTestId('m-overlay')
    const panel = screen.getByRole('dialog')
    act(() => {
      overlay.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    expect(onClose).toHaveBeenCalledTimes(1)
    act(() => {
      panel.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders the footer when provided', () => {
    render(
      <Modal title="t" onClose={vi.fn()} testId="m" footer={<button type="button">Save</button>}>
        body
      </Modal>,
    )
    expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument()
  })

  it('moves focus into the dialog when opened', () => {
    render(
      <Modal title="t" onClose={vi.fn()} testId="m">
        body
      </Modal>,
    )
    const dialog = screen.getByRole('dialog')
    expect(dialog.contains(document.activeElement as HTMLElement)).toBe(true)
  })
})

// #123: at ≤768px the dialog fills the viewport (square corners, no
// max-width cap); ≥769px keeps the centered, max-w sizing. The behavior
// contract (Escape / overlay / focus / aria) above is unchanged.
describe('Modal — mobile full-screen (issue #123)', () => {
  function stubMatchMedia(matches: boolean) {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
      })),
    )
  }

  afterEach(() => vi.unstubAllGlobals())

  it('goes full-screen at mobile widths', () => {
    stubMatchMedia(true)
    render(
      <Modal title="t" onClose={vi.fn()} testId="m">
        body
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    expect(panel).toHaveClass('max-w-none')
    expect(panel).toHaveClass('h-full')
    expect(panel).not.toHaveClass('max-h-[85vh]')
  })

  it('fills the remaining height with the body so the footer stays pinned at the bottom (mobile)', () => {
    stubMatchMedia(true)
    render(
      <Modal
        title="t"
        onClose={vi.fn()}
        testId="m"
        footer={
          <button type="button">ok</button>
        }
      >
        body
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    // The scrollable body is the dialog's middle section (header, body, footer).
    const body = panel.children[1]
    expect(body).toHaveClass('flex-1')
    expect(body).toHaveClass('overflow-y-auto')
    // Footer present and last in the panel.
    expect(panel.children[panel.children.length - 1]).toContainElement(
      screen.getByRole('button', { name: 'ok' }),
    )
  })

  it('keeps the centered max-w sizing at desktop widths', () => {
    stubMatchMedia(false)
    render(
      <Modal title="t" onClose={vi.fn()} testId="m">
        body
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    expect(panel).toHaveClass('max-h-[85vh]')
    expect(panel).toHaveClass('max-w-lg') // default md size
    expect(panel).not.toHaveClass('h-full')
  })
})

describe('ConfirmDialog', () => {
  it('fires onConfirm from the confirm button and onCancel from cancel', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(
      <ConfirmDialog
        title="Delete release?"
        message="This cannot be undone."
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    )
    const user = userEvent.setup()
    await user.click(screen.getByTestId('confirm-confirm'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    await user.click(screen.getByTestId('confirm-cancel'))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('shows the destructive tone and custom labels', () => {
    render(
      <ConfirmDialog
        title="Delete"
        message="msg"
        tone="danger"
        confirmLabel="Delete"
        cancelLabel="Keep it"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const confirm = screen.getByTestId('confirm-confirm')
    expect(confirm).toHaveTextContent('Delete')
    expect(confirm).toHaveClass('bg-error')
    expect(screen.getByTestId('confirm-cancel')).toHaveTextContent('Keep it')
  })

  it('disables the confirm button while busy', () => {
    render(
      <ConfirmDialog
        title="Delete"
        message="msg"
        busy
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByTestId('confirm-confirm')).toBeDisabled()
  })

  it('closes via its inherited Escape handling', async () => {
    const onCancel = vi.fn()
    render(
      <ConfirmDialog title="Delete" message="msg" onConfirm={vi.fn()} onCancel={onCancel} />,
    )
    const user = userEvent.setup()
    await user.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalledTimes(1)
  })
})
