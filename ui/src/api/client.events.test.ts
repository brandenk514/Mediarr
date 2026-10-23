import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connectEvents } from './client'
import { setToken } from './store'
import type { EventFrame } from './types'

// A just-enough WebSocket stand-in: records the URL it was opened with, and
// exposes the handlers the helper wires so tests can drive open/message/close.
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  readyState = 0
  onopen: ((ev?: unknown) => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  close() {
    this.readyState = 3
    this.onclose?.()
  }

  // test helpers
  fireOpen() {
    this.readyState = 1
    this.onopen?.()
  }
  sendFrame(frame: EventFrame) {
    this.onmessage?.({ data: JSON.stringify(frame) })
  }
  fireClose() {
    this.readyState = 3
    this.onclose?.()
  }
}

function lastWs(): FakeWebSocket {
  return FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
}

beforeEach(() => {
  vi.useFakeTimers()
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('connectEvents', () => {
  it('dials the events URL with the bearer token as ?token= (browser path)', () => {
    setToken('ma_secret')
    const close = connectEvents(() => {})
    const ws = lastWs()
    expect(ws.url).toBe(`ws://${window.location.host}/api/v1/events?token=ma_secret`)
    close()
  })

  it('delivers parsed frames to onEvent and reports open status', () => {
    setToken('ma_secret')
    const onEvent = vi.fn()
    const onStatus = vi.fn()
    const close = connectEvents(onEvent, onStatus)
    const ws = lastWs()
    ws.fireOpen()
    ws.sendFrame({ type: 'hello', at: '2026-10-11T12:00:00Z' })
    const queuePayload = {
      movie_id: 101,
      queue_id: 1,
      release_title: 'rel',
      indexer: 'idx',
      client: 'qBittorrent',
      state: 'downloading',
      progress: 42,
    } as const
    ws.sendFrame({ type: 'queue.updated', payload: queuePayload, at: '2026-10-11T12:00:01Z' })

    expect(onStatus).toHaveBeenNthCalledWith(1, 'connecting')
    expect(onStatus).toHaveBeenNthCalledWith(2, 'open')
    expect(onEvent).toHaveBeenCalledTimes(2)
    expect(onEvent.mock.calls[0][0].type).toBe('hello')
    expect(onEvent.mock.calls[1][0].payload).toEqual(queuePayload)
    close()
  })

  it('ignores a malformed frame without dropping the stream', () => {
    const onEvent = vi.fn()
    const close = connectEvents(onEvent)
    const ws = lastWs()
    ws.fireOpen()
    ;(ws.onmessage as (ev: { data: string }) => void)({ data: 'not-json' })
    expect(onEvent).not.toHaveBeenCalled()
    ws.sendFrame({ type: 'history.appended', at: '2026-10-11T12:00:02Z' })
    expect(onEvent).toHaveBeenCalledTimes(1)
    close()
  })

  it('reconnects with capped backoff after an unexpected close', () => {
    setToken('ma_secret')
    const close = connectEvents(() => {})
    const first = lastWs()
    first.fireOpen()
    first.fireClose() // unexpected → schedules a reconnect (1s backoff, retry 0)

    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)
    const second = lastWs()
    expect(second).not.toBe(first)

    // the second close backs off 2s (retry 1)
    second.fireClose()
    vi.advanceTimersByTime(2000)
    expect(FakeWebSocket.instances).toHaveLength(3)

    close()
  })

  it('stop() closes the socket and never reconnects', () => {
    setToken('ma_secret')
    const onStatus = vi.fn()
    const close = connectEvents(() => {}, onStatus)
    const first = lastWs()
    first.fireOpen()
    close()
    // let any stray timer fire — nothing should dial again
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(onStatus).toHaveBeenLastCalledWith('closed')
  })
})
