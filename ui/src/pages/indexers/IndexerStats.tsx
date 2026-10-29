import { Link } from 'react-router-dom'

// IndexerStatsPage is the PR1 stub for the Indexers ▸ Stats sub-page. The real
// per-indexer health rollup (calls / successes / failures / latency, live from
// the shared HealthTracker) lands in PR3; for PR1 the rail item exists and is
// reachable so the hub IA is complete, and it points the user back to the list.
export function IndexerStatsPage() {
  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="indexers-stats-title">Indexer Stats</h2>
      </div>
      <div className="card" data-testid="indexers-stats-stub">
        <h3>Per-indexer stats are coming in PR3</h3>
        <p className="dim">
          This page will show the live health rollup for every indexer — call
          counts, success / failure breakdown, and latency — read from the same
          tracker the on-demand and periodic probes record into.
        </p>
        <Link to="/indexers" data-testid="indexers-stats-back">
          ← Back to Indexers
        </Link>
      </div>
    </div>
  )
}
