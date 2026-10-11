// LiveBadge shows the /api/v1/events WebSocket phase for the dashboard (#40:
// "WebSocket subscription for live queue/history updates"). open = live,
// connecting = establishing / reconnecting, closed = caller stopped it.
export function LiveBadge({ status }: { status: 'connecting' | 'open' | 'closed' }) {
  const map = {
    open: { cls: 'pill-ok', label: 'live' },
    connecting: { cls: 'pill-warn', label: 'connecting…' },
    closed: { cls: 'pill-idle', label: 'offline' },
  } as const
  const { cls, label } = map[status]
  return (
    <span className={`pill ${cls}`} data-testid="live-badge">
      {label}
    </span>
  )
}
