import { Link } from 'react-router-dom'

// AppsSection is the Prowlarr PR4 "Apps" placeholder. Prowlarr's Apps page
// (per-app category/sync behavior) is deliberately NOT a 1:1 drop-in: in
// Mediarr the per-kind category/sync behavior lives in the TV / Movies /
// Music / Books settings — one surface, four kinds. This card makes the
// omission explicit rather than shipping a non-functional page.
export function AppsSection() {
  return (
    <div className="card" data-testid="apps-card">
      <h3>Connected apps (legacy)</h3>
      <p className="dim">
        Prowlarr's Apps section is not a drop-in: its per-app category and sync
        settings are replaced by the per-kind settings in this app (one
        surface, four kinds — TV, Movies, Music, Books). There is nothing to
        configure here.
      </p>
      <Link to="/settings/media-management" data-testid="apps-settings-link">
        Open the per-kind settings →
      </Link>
    </div>
  )
}
