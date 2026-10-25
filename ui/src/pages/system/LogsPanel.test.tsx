import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { openSystemLogsStream } from '../../api/client'
import type { SystemLogLine } from '../../api/client'
import { LogsPanel } from './LogsPanel'

vi.mock('../../api/client', () => ({
  openSystemLogsStream: vi.fn(),
}))

const LINES: SystemLogLine[] = [
  { time: '2026-10-25T01:00:00Z', level: 'info', message: 'server started' },
  { time: '2026-10-25T01:00:05Z', level: 'warn', message: 'health check degraded' },
]

// streamOnce makes the next openSystemLogsStream call deliver LINES once the
// stream opens, and returns the close handle.
function streamOnce(closeSpy?: ReturnType<typeof vi.fn>) {
  const close = closeSpy ?? vi.fn()
  ;(openSystemLogsStream as ReturnType<typeof vi.fn>).mockImplementation(
    (onLines: (l: SystemLogLine[]) => void, onStatus: (s: unknown) => void) => {
      onStatus({ state: 'open' })
      onLines(LINES)
      return close
    },
  )
  return close
}

beforeEach(() => {
  vi.clearAllMocks()
  // Default: the stream opens and delivers the backlog once.
  streamOnce()
})

describe('LogsPanel', () => {
  it('streams the backlog into the viewer with a live badge', async () => {
    render(<LogsPanel />)
    const line = await screen.findByTestId('log-line-0')
    expect(line).toHaveTextContent('server started')
    expect(screen.getByTestId('log-line-1')).toHaveTextContent('health check degraded')
    expect(screen.getByTestId('logs-status')).toHaveTextContent('live')
    expect(screen.getByTestId('logs-count')).toHaveTextContent('2 lines')
  })

  it('clears the viewport', async () => {
    const user = userEvent.setup()
    render(<LogsPanel />)
    await screen.findByTestId('log-line-0')
    await user.click(screen.getByTestId('logs-clear-btn'))
    expect(screen.getByTestId('logs-count')).toHaveTextContent('0 lines')
    expect(screen.queryByTestId('log-line-0')).not.toBeInTheDocument()
  })

  it('stops appending lines while paused', async () => {
    render(<LogsPanel />)
    await screen.findByTestId('log-line-0')
    // A late frame arrives after the initial backlog.
    act(() => {
      const onLines = (openSystemLogsStream as ReturnType<typeof vi.fn>).mock.calls[0][0]
      onLines([{ time: '2026-10-25T01:01:00Z', level: 'info', message: 'late' }])
    })
    expect(screen.getByTestId('logs-count')).toHaveTextContent('3 lines')

    const user = userEvent.setup()
    await user.click(screen.getByTestId('logs-pause-btn'))
    act(() => {
      const onLines = (openSystemLogsStream as ReturnType<typeof vi.fn>).mock.calls[0][0]
      onLines([{ time: '2026-10-25T01:02:00Z', level: 'info', message: 'ignored' }])
    })
    expect(screen.getByTestId('logs-count')).toHaveTextContent('3 lines')
  })

  it('shows the error-gated state when the stream fails, then recovers on retry', async () => {
    const user = userEvent.setup()
    // First attempt: the endpoint is absent (P1 dev stack).
    ;(openSystemLogsStream as ReturnType<typeof vi.fn>).mockImplementation(
      (_onLines: unknown, onStatus: (s: unknown) => void) => {
        onStatus({ state: 'error', error: 'log stream failed (404)' })
        return vi.fn()
      },
    )
    render(<LogsPanel />)
    await screen.findByTestId('system-logs-error')
    expect(screen.getByText('Log Files unavailable')).toBeInTheDocument()
    expect(screen.getByText('log stream failed (404)')).toBeInTheDocument()

    // Retry: the endpoint is live now.
    streamOnce()
    await user.click(screen.getByTestId('logs-retry'))
    await screen.findByTestId('system-logs')
  })

  it('closes the stream on unmount', async () => {
    const close = streamOnce()
    const { unmount } = render(<LogsPanel />)
    await screen.findByTestId('log-line-0')
    unmount()
    expect(close).toHaveBeenCalled()
  })
})
