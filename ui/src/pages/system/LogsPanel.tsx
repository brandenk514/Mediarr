import { useEffect, useRef, useState } from 'react'
import { openSystemLogsStream } from '../../api/client'
import type { LogStreamStatus, SystemLogLine } from '../../api/client'

// LogsPanel is System > Log Files: a live streaming log viewer over the
// /api/v1/system/logs SSE endpoint (the server's in-memory ring buffer —
// backlog first, then live lines). EventSource can't carry the bearer
// header, so the client helper uses fetch + ReadableStream instead.
const MAX_LINES = 1000

export function LogsPanel() {
  const [lines, setLines] = useState<SystemLogLine[]>([])
  const [status, setStatus] = useState<LogStreamStatus>({ state: 'connecting' })
  const [error, setError] = useState<string | null>(null)
  const [paused, setPaused] = useState(false)
  const [nonce, setNonce] = useState(0)
  const viewportRef = useRef<HTMLDivElement>(null)
  const pausedRef = useRef(false)

  // The stream closes on unmount; opened once per mount attempt (nonce bumps
  // on Retry).
  useEffect(() => {
    pausedRef.current = false
    setPaused(false)
    setError(null)
    const close = openSystemLogsStream(
      (batch) => {
        if (pausedRef.current) return
        setLines((prev) => [...prev, ...batch].slice(-MAX_LINES))
      },
      (s) => {
        if (s.state === 'error') setError(s.error ?? 'the log stream could not be opened')
        else setStatus(s)
      },
    )
    return () => close()
  }, [nonce])

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  // Keep the viewport pinned to the newest line while streaming.
  useEffect(() => {
    if (paused) return
    const el = viewportRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines, paused])

  if (error) {
    return (
      <div className="card" data-testid="system-logs-error">
        <h2>Log Files unavailable</h2>
        <p className="error">{error}</p>
        <button data-testid="logs-retry" onClick={() => setNonce((n) => n + 1)}>
          Retry
        </button>
      </div>
    )
  }

  return (
    <div className="card" data-testid="system-logs">
      <h3 style={{ margin: '0 0 0.5rem' }}>
        Log Files{' '}
        <span
          className={`pill ${status.state === 'open' ? 'pill-ok' : status.state === 'closed' ? 'pill-unknown' : 'pill-warn'}`}
          data-testid="logs-status"
        >
          {status.state === 'open' ? 'live' : status.state === 'closed' ? 'stopped' : 'connecting'}
        </span>
      </h3>
      <p className="dim" style={{ marginTop: 0 }}>
        Live stream of the instance log (last {MAX_LINES} lines), newest at the bottom.
      </p>

      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', margin: '0.5rem 0' }}>
        <button className="ghost" data-testid="logs-pause-btn" onClick={() => setPaused((p) => !p)}>
          {paused ? 'Resume' : 'Pause'}
        </button>
        <button className="ghost" data-testid="logs-clear-btn" onClick={() => setLines([])}>
          Clear
        </button>
        <span className="dim small" data-testid="logs-count">
          {lines.length} lines
        </span>
      </div>

      <div
        ref={viewportRef}
        data-testid="logs-viewport"
        className="code"
        style={{ height: '26rem', overflowY: 'auto', fontSize: '0.78rem' }}
      >
        {lines.length === 0 ? (
          <span className="dim">No lines yet — waiting for the stream…</span>
        ) : (
          lines.map((l, i) => (
            <div key={i} data-testid={`log-line-${i}`} className="log-line">
              <span className="dim">{new Date(l.time).toLocaleTimeString()}</span>{' '}
              <span className={`log-level log-level-${l.level}`}>{l.level}</span> {l.message}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
