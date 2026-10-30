import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../../api/client'
import type { NotificationSink } from '../../../api/types'
import { NotificationsSection } from './NotificationsSection'

// Mock the typed client so the section talks to in-test fakes.
vi.mock('../../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    body: string
    constructor(status: number, body: string) {
      super(body)
      this.status = status
      this.body = body
    }
  },
  client: {
    listNotifications: vi.fn(),
    createNotification: vi.fn(),
    updateNotification: vi.fn(),
    deleteNotification: vi.fn(),
    testNotification: vi.fn(),
  },
}))

function sink(partial: Partial<NotificationSink> = {}): NotificationSink {
  return {
    id: 1,
    name: 'ntfy home',
    kind: 'webhook',
    url: 'https://ntfy.sh/mediarr',
    section_id: '',
    token: '',
    token_set: true,
    user_agent: '',
    enabled: true,
    created_at: '2026-10-29T00:00:00Z',
    ...partial,
  }
}

function renderSection() {
  return render(
    <MemoryRouter>
      <NotificationsSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('NotificationsSection', () => {
  it('shows the empty state with an add button on a fresh install', async () => {
    renderSection()
    expect(await screen.findByTestId('notif-empty')).toBeInTheDocument()
    expect(screen.getByTestId('notif-empty-add')).toBeInTheDocument()
  })

  it('lists sinks with the masked token (presence only)', async () => {
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValue([
      sink(),
      sink({ id: 2, name: 'plex', kind: 'plex', url: 'http://plex:32400', token_set: false }),
    ])
    renderSection()
    const table = await screen.findByTestId('notif-table')
    expect(table).toBeInTheDocument()
    expect(screen.getByTestId('notif-row-1')).toHaveTextContent('ntfy home')
    // The token is never echoed — presence is shown as "set"/"—".
    expect(screen.getByTestId('notif-token-1')).toHaveTextContent('set')
    expect(screen.getByTestId('notif-token-2')).toHaveTextContent('—')
  })

  it('creates a sink from the form', async () => {
    const user = userEvent.setup()
    ;(client.createNotification as ReturnType<typeof vi.fn>).mockResolvedValue(sink({ id: 9 }))
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValueOnce([])
    renderSection()
    await screen.findByTestId('notif-empty')
    await user.click(screen.getByTestId('notif-add'))
    await user.type(screen.getByTestId('notif-f-name'), 'ntfy home')
    await user.type(screen.getByTestId('notif-f-url'), 'https://ntfy.sh/mediarr')
    await user.click(screen.getByTestId('notif-save'))
    await waitFor(() => {
      expect(client.createNotification).toHaveBeenCalledWith({
        name: 'ntfy home',
        kind: 'webhook',
        url: 'https://ntfy.sh/mediarr',
        section_id: '',
        token: '',
        user_agent: '',
        enabled: true,
      })
    })
  })

  it('shows the Plex section field only for the plex kind', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('notif-empty')
    await user.click(screen.getByTestId('notif-add'))
    expect(screen.queryByTestId('notif-f-section')).not.toBeInTheDocument()
    await user.selectOptions(screen.getByTestId('notif-f-kind'), 'plex')
    expect(screen.getByTestId('notif-f-section')).toBeInTheDocument()
  })

  it('edits a sink with a blank (keep-stored) token', async () => {
    const user = userEvent.setup()
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValue([sink()])
    ;(client.updateNotification as ReturnType<typeof vi.fn>).mockResolvedValue(sink())
    renderSection()
    await screen.findByTestId('notif-row-1')
    await user.click(screen.getByTestId('notif-edit-1'))
    // The form re-opens seeded from the row; the token field is blank on edit.
    const token = screen.getByTestId('notif-f-token')
    expect(token).toHaveValue('')
    // Whole-set via fireEvent.change — user.clear() on seeded inputs is flaky
    // in this codebase (the userEvent pitfall in the dev-stack skill).
    fireEvent.change(screen.getByTestId('notif-f-name'), { target: { value: 'renamed' } })
    await user.click(screen.getByTestId('notif-save'))
    await waitFor(() => {
      expect(client.updateNotification).toHaveBeenCalledWith(1, {
        name: 'renamed',
        kind: 'webhook',
        url: 'https://ntfy.sh/mediarr',
        section_id: '',
        token: '',
        user_agent: '',
        enabled: true,
      })
    })
  })

  it('runs the per-sink test and shows the outcome', async () => {
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValue([sink()])
    ;(client.testNotification as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true })
    renderSection()
    await screen.findByTestId('notif-row-1')
    fireEvent.click(screen.getByTestId('notif-test-1'))
    expect(await screen.findByTestId('notif-test-result-1')).toHaveTextContent('Delivered')
  })

  it('deletes a sink', async () => {
    const user = userEvent.setup()
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockResolvedValue([sink()])
    ;(client.deleteNotification as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    renderSection()
    await screen.findByTestId('notif-row-1')
    await user.click(screen.getByTestId('notif-del-1'))
    await waitFor(() => expect(client.deleteNotification).toHaveBeenCalledWith(1))
  })

  it('shows a section error state when the load fails', async () => {
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'boom'),
    )
    renderSection()
    expect(await screen.findByTestId('notif-error')).toBeInTheDocument()
  })

  it('offers a re-login link on 401', async () => {
    ;(client.listNotifications as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'unauthorized'),
    )
    renderSection()
    expect(await screen.findByTestId('notif-error')).toBeInTheDocument()
    expect(screen.getByTestId('notif-error-retry')).toBeInTheDocument()
  })
})
