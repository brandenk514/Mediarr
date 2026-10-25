import { NavLink, useLocation } from 'react-router-dom'
import { SettingsLayout } from '../../components/ui'
import { GeneralSection } from './sections/GeneralSection'
import { UISection } from './sections/UISection'
import { QualitySection } from './sections/QualitySection'
import { ProfilesSection } from './sections/ProfilesSection'
import { TagsSection } from './sections/TagsSection'
import { MediaManagementSection } from './sections/MediaManagementSection'
import { IndexersSection } from './sections/IndexersSection'
import { DownloadClientsSection } from './sections/DownloadClientsSection'

// The 13 Sonarr settings sections (the settings IA contract, P1). Each is a
// route (/settings/:section). P1 established the nav + layout; P5 decomposes
// the legacy monolithic settings page into per-section forms: the generic
// sections ride the per-section document endpoints, while indexers and
// download-clients keep their dedicated endpoints.
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

// renderSection maps a slug to its P5 section component. The sections that
// ship in later waves (custom-formats, import-lists, connect, metadata,
// metadata-source) still render the "coming" placeholder until their forms
// land.
function renderSection(slug: string, label: string) {
  switch (slug) {
    case 'media-management':
      return <MediaManagementSection />
    case 'profiles':
      return <ProfilesSection />
    case 'quality':
      return <QualitySection />
    case 'tags':
      return <TagsSection />
    case 'general':
      return <GeneralSection />
    case 'ui':
      return <UISection />
    case 'indexers':
      return <IndexersSection />
    case 'download-clients':
      return <DownloadClientsSection />
    default:
      return (
        <div className="empty-state" data-testid={`settings-empty-${slug}`}>
          <h3>{label}</h3>
          <p>This settings section ships with the settings parity build (P5).</p>
        </div>
      )
  }
}

// SettingsShell is the settings IA: a left section nav + a content pane,
// framed by the shared SettingsLayout. The default section (media-management)
// matches the P1 e2e contract.
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
      {renderSection(current, label)}
    </SettingsLayout>
  )
}
