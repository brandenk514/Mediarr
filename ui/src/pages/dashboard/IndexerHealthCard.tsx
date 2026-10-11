import type { IndexerHealth } from '../../api/types'

// IndexerHealthCard is the "indexer health at a glance" tile (#40). The
// tri-state pill maps directly to the server's status: ok (green), error
// (red, with the last error surfaced), idle (grey, never observed).
export function IndexerHealthCard({ indexer }: { indexer: IndexerHealth }) {
  return (
    <div className="card indexer-card" data-testid={`indexer-${indexer.name}`}>
      <div className="lib-head">
        <span className="lib-label">{indexer.name}</span>
        <span className={`pill pill-${indexer.status}`} data-testid={`indexer-status-${indexer.name}`}>
          {indexer.status}
        </span>
      </div>
      <div className="indexer-stats">
        <span>{indexer.successes} ok</span>
        <span>{indexer.failures} failed</span>
        {indexer.last_error && (
          <span className="error small" title={indexer.last_error}>
            {indexer.last_error}
          </span>
        )}
      </div>
    </div>
  )
}
