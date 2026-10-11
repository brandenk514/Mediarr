import type { DashboardLibrary } from '../../api/types'

// LibraryCard is one per-kind tile on the dashboard (#40): the human label,
// the top-level item count, and the two live numbers the overview promises —
// active wants and in-flight queue depth.
export function LibraryCard({ library }: { library: DashboardLibrary }) {
  return (
    <div className="card lib-card" data-testid={`lib-${library.kind}`}>
      <div className="lib-head">
        <span className="lib-label">{library.label}</span>
        <span className="lib-count">{library.count}</span>
      </div>
      <div className="lib-stats">
        <span>
          <strong>{library.wants}</strong> <span className="dim">wanted</span>
        </span>
        <span>
          <strong>{library.queue}</strong> <span className="dim">in queue</span>
        </span>
      </div>
    </div>
  )
}
