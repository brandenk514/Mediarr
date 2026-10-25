import { NavLink, useLocation } from 'react-router-dom'
import { SettingsPage } from './Settings'
import { SettingsLayout } from '../../components/ui'

// The 13 Sonarr settings sections (the settings IA contract, P1). Each is a
// route (/settings/:section). P1 establishes the nav + layout + unsaved-changes
// bar; per-section forms ship in P5. The first section carries the app's
// current settings for now (P5 decomposes it into per-section forms).
export const SETTINGS_SECTIONS: { slug: string; label: string }[] = [
  { slug: 'media-management', label: 'Media Management' },
  { slug: 'profiles', label: 'Profiles' },
  { slug: 'quality', label: 'Quality' },
  { slug: 'custom-formats', label: 'Custom Formats' },
  { slug: 'indexers', label: 'Indexers' },
  { slug: 'download-clients', label: 'Download Clients' },
  { slug: 'import-lists', label: 'Import Lists' },
  { slug: 'connect', label: 'Connect' },
  { slug: 'metadata', label: 'Metadata' },
  { slug: 'metadata-source', label: 'Metadata Source' },
  { slug: 'tags', label: 'Tags' },
  { slug: 'general', label: 'General' },
  { slug: 'ui', label: 'UI' },
]

// SettingsShell is the settings IA (P1): a left section nav + a content pane
// with the unsaved-changes bar, framed by the shared SettingsLayout. The
// default section (media-management) hosts the current settings until P5
// decomposes it into per-section forms.
export function SettingsShell() {
  const { pathname } = useLocation()
  const current = pathname.split('/')[2] || 'media-management'
  const label = SETTINGS_SECTIONS.find((s) => s.slug === current)?.label ?? 'Settings'

  return (
    <SettingsLayout
      nav={SETTINGS_SECTIONS.map((s) => (
        <NavLink
          key={s.slug}
          to={`/settings/${s.slug}`}
          className={s.slug === current ? 'active' : ''}
          data-testid={`settings-nav-${s.slug}`}
        >
          {s.label}
        </NavLink>
      ))}
    >
      <h2 style={{ marginBottom: '0.8rem' }}>{label}</h2>
      {current === 'media-management' ? (
        <SettingsPage />
      ) : (
        <div className="empty-state" data-testid={`settings-empty-${current}`}>
          <h3>{label}</h3>
          <p>This settings section ships with the settings parity build (P5).</p>
        </div>
      )}
    </SettingsLayout>
  )
}
