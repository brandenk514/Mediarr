// Small display helpers shared by UI cards. Pure (no I/O) so they're trivially
// testable. `now` is injectable for deterministic tests.

export function timeAgo(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const s = Math.round((now - t) / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.round(h / 24)
  if (d < 7) return `${d}d ago`
  return new Date(t).toLocaleDateString()
}

// Progress label for a queue row: 0 and 100 read as state (not a percentage);
// in-between values show the percent.
export function progressLabel(progress: number): string {
  if (progress <= 0) return ''
  if (progress >= 100) return '100%'
  return `${progress}%`
}
