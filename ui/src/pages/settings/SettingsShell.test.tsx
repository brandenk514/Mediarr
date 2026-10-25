import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { SettingsShell, SETTINGS_SECTIONS } from './SettingsShell'

// Mock the client so the dispatched section components don't hit the network.
// getSettingsSection returns a shape-matched document per slug — the real
// server merges stored values over section defaults, so the mocks mirror a
// fresh-install response.
vi.mock('../../api/client', () => ({
  client: {
    getDownloadClient: vi.fn().mockResolvedValue({
      kind: 'mock',
      qb_base: '',
      qb_username: '',
      qb_api_key_set: false,
      qb_password_set: false,
      sab_base: '',
      sab_api_key_set: false,
      sab_complete_dir: '',
    }),
    getLibraryOverrides: vi.fn().mockResolvedValue({
      movie: { path: '', quality_profile: '', checksum_verify: null },
      tv: { path: '', quality_profile: '', checksum_verify: null },
      music: { path: '', quality_profile: '', checksum_verify: null },
      book: { path: '', quality_profile: '', checksum_verify: null },
    }),
    listIndexers: vi.fn().mockResolvedValue([]),
    listRootFolders: vi.fn().mockResolvedValue([]),
    getSettingsSection: vi.fn(async (section: string) => {
      switch (section) {
        case 'general':
          return {
            host: '0.0.0.0',
            port: 8080,
            base_url: '',
            ssl: false,
            auth_enabled: true,
            auth_pinning: true,
          }
        case 'ui':
          return { default_view: 'dashboard', date_format: 'YYYY-MM-DD', time_format: '24h', items_per_page: 50 }
        case 'quality':
          return {
            levels: [
              { id: 1, name: 'SD', source: 'web', resolution: '480p', size_limit_mb: 0 },
              { id: 2, name: '720p', source: 'web', resolution: '720p', size_limit_mb: 0 },
            ],
          }
        case 'profiles':
          return {
            profiles: [
              { id: 1, name: 'Default', cutoff: 2, items: [{ quality: 1, approved: true }, { quality: 2, approved: false }] },
            ],
          }
        case 'tags':
          return { tags: [{ id: 1, label: 'family', description: 'Family TV' }] }
        case 'media-management':
          return {
            naming: {
              series: 'Series',
              episode: 'Episode',
              movie: 'Movie',
              album: 'Album',
              track: 'Track',
              author: 'Author',
              title: 'Title',
            },
            file_management: {
              min_file_age_hours: 0,
              remove_empty_dirs: true,
              import_extra_files: false,
              rename_series: false,
            },
            remote_path_mappings: [],
          }
        default:
          return {}
      }
    }),
    putSettingsSection: vi.fn(),
    getTagUsage: vi.fn().mockResolvedValue({ id: 1, label: 'family', series: 3, episodes: 42 }),
  },
}))

function renderShell(path = '/settings/media-management') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <SettingsShell />
    </MemoryRouter>,
  )
}

describe('SettingsShell', () => {
  it('renders the 13-section nav (the P5 settings IA contract)', () => {
    renderShell()
    // All 13 sections are present as nav items.
    expect(SETTINGS_SECTIONS).toHaveLength(13)
    for (const s of SETTINGS_SECTIONS) {
      expect(screen.getByTestId(`settings-nav-${s.slug}`)).toHaveTextContent(s.label)
    }
  })

  it('renders the default section (media-management)', () => {
    renderShell('/settings/media-management')
    // The default section's form mounts (the card testid is present).
    expect(screen.getByTestId('media-management-card')).toBeInTheDocument()
  })

  it('renders each implemented section for its slug', async () => {
    const cases: [string, string][] = [
      ['/settings/general', 'general-card'],
      ['/settings/ui', 'ui-card'],
      ['/settings/quality', 'quality-card'],
      ['/settings/profiles', 'profiles-card'],
      ['/settings/tags', 'tags-card'],
      ['/settings/indexers', 'idx-card'],
      ['/settings/download-clients', 'dc-card'],
    ]
    for (const [path, card] of cases) {
      const { unmount } = renderShell(path)
      expect(await screen.findByTestId(card)).toBeInTheDocument()
      unmount()
    }
  })

  it('renders the placeholder for sections that ship in later waves', async () => {
    renderShell('/settings/connect')
    expect(await screen.findByTestId('settings-empty-connect')).toBeInTheDocument()
  })
})
