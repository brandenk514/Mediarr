import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearToken, setToken } from './store'
import { openSystemLogsStream } from './client'
import type { SystemLogLine } from './client'

// Client-level tests for the SSE log-stream helper (the /api/v1/system/logs
// endpoint): frame parsing over a stubbed ReadableStream, header injection,
// and error mapping. Component behaviour (rendering, pause, retry) is
// covered in src/pages/system/LogsPanel.test.tsx.

afterEach(() => {
  clearToken()
  vi.unstubAllGlobals()
})

function sseResponse(frames: string[]): Response {
  // One chunk per frame so the reader gets them incrementally.
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) controller.enqueue(new TextEncoder().encode(f))
      controller.close()
    },
  })
  return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}

describe('openSystemLogsStream', () => {
  it('parses SSE frames (backlog + live) into line batches', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        () =>
          Promise.resolve(
            sseResponse([
              'event: backlog\ndata: [{"time":"2026-10-25T01:00:00Z","level":"info","message":"booted"}]\n\n',
              'data: [{"time":"2026-10-25T01:00:05Z","level":"warn","message":"degraded"}]\n\n',
            ]),
          ),
      ),
    )
    const batches: SystemLogLine[][] = []
    const statuses: string[] = []
    openSystemLogsStream(
      (l) => batches.push(l),
      (s) => statuses.push(s.state),
    )
    await vi.waitFor(() => expect(batches).toHaveLength(2))
    expect(batches[0][0].message).toBe('booted')
    expect(batches[1][0].level).toBe('warn')
    expect(statuses).toEqual(['connecting', 'open', 'closed'])
  })

  it('sends the bearer token in the header', async () => {
    setToken('ma_secret')
    let auth: string | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: unknown, init?: RequestInit) => {
        auth = (init?.headers as Record<string, string>)?.Authorization
        return Promise.resolve(sseResponse([]))
      }),
    )
    openSystemLogsStream(() => {}, () => {})
    await vi.waitFor(() => expect(auth).toBe('Bearer ma_secret'))
  })

  it('maps a non-2xx response (e.g. 404 on a P1 build) to an error status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 404 }))),
    )
    const statuses: string[] = []
    const errors: string[] = []
    openSystemLogsStream(
      () => {},
      (s) => {
        statuses.push(s.state)
        if (s.state === 'error') errors.push(s.error ?? '')
      },
    )
    await vi.waitFor(() => expect(statuses).toContain('error'))
    expect(errors[0]).toContain('404')
  })
})
