import { Link } from 'react-router-dom'

// IndexersSection is now a hub pointer (Prowlarr PR1). The indexer list is the
// FIRST-CLASS Indexers hub at /indexers (the list IS the home), not a settings
// sub-section. This keeps the settings-shell contract (13 slugs, and the
// SettingsShell `idx-card` testid) intact while routing the user to the hub,
// which owns the full table, toolbar, A–Z jump, add/edit modal, and row actions.
export function IndexersSection() {
  return (
    <div className="card" data-testid="idx-card">
      <h3>Indexers</h3>
      <p className="dim">
        Indexers are managed in the Indexers hub — the first-class list page with
        the full table, toolbar, A–Z jump, and summary. It has moved out of
        Settings.
      </p>
      <Link to="/indexers" data-testid="idx-hub-link">
        Open Indexers hub →
      </Link>
    </div>
  )
}
