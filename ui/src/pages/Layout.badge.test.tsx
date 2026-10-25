import { render, renderHook, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import { Rail, useSystemBadge } from './Layout'

// The orange System nav badge is the count of non-ok health checks
// (warning + error). A P1-only dev stack answers 404 -> no badge.
vi.mock('../api/client', () => ({
  client: {
    getSystemHealth: vi.fn(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useSystemBadge (orange System nav badge)', () => {
  it('counts warning + error as non-ok', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({
      checks: [],
      ok: 1,
      warning: 2,
      error: 1,
    })
    const { result } = renderHook(() => useSystemBadge())
    await vi.waitFor(() => expect(result.current).toBe(3))
  })

  it('returns 0 when every check is ok (no badge)', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({
      checks: [],
      ok: 3,
      warning: 0,
      error: 0,
    })
    const { result } = renderHook(() => useSystemBadge())
    await vi.waitFor(() => expect(result.current).toBe(0))
  })

  it('returns 0 when the health endpoint is unavailable (P1 dev stack 404)', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Not Found'))
    const { result } = renderHook(() => useSystemBadge())
    await vi.waitFor(() => expect(result.current).toBe(0))
  })
})

describe('Rail — System badge rendering', () => {
  it('shows the orange count badge on the System item when checks are non-ok', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({
      checks: [],
      ok: 1,
      warning: 2,
      error: 1,
    })
    render(
      <MemoryRouter initialEntries={['/tv']}>
        <Rail />
      </MemoryRouter>,
    )
    const badge = await screen.findByTestId('rail-system-badge')
    expect(badge).toHaveTextContent('3')
  })

  it('hides the badge when all checks are ok', async () => {
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockResolvedValue({
      checks: [],
      ok: 3,
      warning: 0,
      error: 0,
    })
    render(
      <MemoryRouter initialEntries={['/tv']}>
        <Rail />
      </MemoryRouter>,
    )
    await screen.findByText('System')
    expect(screen.queryByTestId('rail-system-badge')).not.toBeInTheDocument()
  })
})
