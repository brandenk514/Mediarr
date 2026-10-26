import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { GeneralSection } from './GeneralSection'
import { UISection } from './UISection'
import { QualitySection } from './QualitySection'
import { ProfilesSection } from './ProfilesSection'
import { TagsSection } from './TagsSection'
import { MediaManagementSection } from './MediaManagementSection'

// Mock the typed client so the sections talk to in-test fakes. The generic
// sections are read-once + save-on-demand over the per-section document
// endpoints; getSettingsSection answers with the server's fresh-install
// documents (defaults merged in).
vi.mock('../../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    constructor(status: number) {
      super('boom')
      this.status = status
    }
  },
  client: {
    getSettingsSection: vi.fn(),
    putSettingsSection: vi.fn(),
    getTagUsage: vi.fn(),
    listRootFolders: vi.fn(),
  },
}))

function freshDoc(section: string): unknown {
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
          { id: 3, name: '1080p', source: 'web', resolution: '1080p', size_limit_mb: 0 },
        ],
      }
    case 'profiles':
      return {
        profiles: [
          {
            id: 1,
            name: 'Default',
            cutoff: 3,
            items: [
              { quality: 1, approved: true },
              { quality: 2, approved: true },
              { quality: 3, approved: false },
            ],
          },
        ],
      }
    case 'tags':
      return { tags: [{ id: 1, label: 'family', description: 'Family TV' }] }
    case 'media-management':
      return {
        naming: {
          series: 'Series',
          episode: 'Episode {s}-{e} {q}',
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
        remote_path_mappings: [{ source: '/srv/torrents', destination: '/data/torrents' }],
      }
    default:
      return {}
  }
}

function renderSection(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (section: string) => freshDoc(section),
  )
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_section: string, doc: unknown) => doc as never,
  )
  ;(client.getTagUsage as ReturnType<typeof vi.fn>).mockResolvedValue({
    id: 1,
    label: 'family',
    series: 3,
    episodes: 42,
  })
  ;(client.listRootFolders as ReturnType<typeof vi.fn>).mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('GeneralSection', () => {
  it('loads defaults and saves the port', async () => {
    const user = userEvent.setup()
    renderSection(<GeneralSection />)
    const port = await screen.findByTestId('gen-port')
    expect(port).toHaveValue(8080)
    // Wait for the document load to complete (the save button arms once
    // loading is done) before editing, so the seeding effect has landed.
    await waitFor(() => expect(screen.getByTestId('general-save')).toBeEnabled())

    // fireEvent.change is deterministic for number inputs (userEvent's clear
    // doesn't clear a controlled number input in jsdom).
    fireEvent.change(port, { target: { value: '9090' } })
    await user.click(screen.getByTestId('general-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'general',
        expect.objectContaining({ port: 9090 }),
      ),
    )
  })
})

describe('UISection', () => {
  it('loads defaults and saves the default view', async () => {
    const user = userEvent.setup()
    renderSection(<UISection />)
    const view = await screen.findByTestId('ui-view')
    expect(view).toHaveValue('dashboard')

    await user.selectOptions(view, 'calendar')
    await user.click(screen.getByTestId('ui-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('ui', {
        default_view: 'calendar',
        date_format: 'YYYY-MM-DD',
        time_format: '24h',
        items_per_page: 50,
      }),
    )
  })
})

describe('QualitySection', () => {
  it('renders the quality ladder and saves a renamed level', async () => {
    const user = userEvent.setup()
    renderSection(<QualitySection />)
    // Three levels render in the table.
    expect(await screen.findByTestId('quality-name-1')).toHaveValue('SD')
    expect(screen.getByTestId('quality-name-3')).toHaveValue('1080p')

    await user.clear(screen.getByTestId('quality-name-1'))
    await user.type(screen.getByTestId('quality-name-1'), 'SD-R')
    await user.click(screen.getByTestId('quality-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'quality',
        {
          levels: [
            { id: 1, name: 'SD-R', source: 'web', resolution: '480p', size_limit_mb: 0 },
            { id: 2, name: '720p', source: 'web', resolution: '720p', size_limit_mb: 0 },
            { id: 3, name: '1080p', source: 'web', resolution: '1080p', size_limit_mb: 0 },
          ],
        },
      ),
    )
  })
})

describe('ProfilesSection', () => {
  it('toggles a level and saves the profile', async () => {
    const user = userEvent.setup()
    renderSection(<ProfilesSection />)
    // The seeded profile renders with its cutoff and level checkboxes.
    expect((await screen.findByTestId('profile-name-1')).tagName).toBe('INPUT')
    const cutoff = screen.getByTestId('profile-cutoff-1') as HTMLSelectElement
    expect(cutoff).toHaveValue('3')

    await user.click(screen.getByTestId('profile-level-1-3'))
    await user.selectOptions(cutoff, '2')
    await user.click(screen.getByTestId('profiles-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'profiles',
        expect.objectContaining({
          profiles: [
            expect.objectContaining({
              id: 1,
              cutoff: 2,
              items: [
                expect.objectContaining({ quality: 1, approved: true }),
                expect.objectContaining({ quality: 2, approved: true }),
                expect.objectContaining({ quality: 3, approved: true }),
              ],
            }),
          ],
        }),
      ),
    )
  })
})

describe('TagsSection', () => {
  it('renders tags with usage and adds a tag', async () => {
    const user = userEvent.setup()
    renderSection(<TagsSection />)
    // The usage cell renders before the per-tag usage fetch settles (the row
    // appears with '—'), so wait for the text itself — a plain findByTestId +
    // sync assertion races the mock's promise and flakes intermittently.
    const usageCell = await screen.findByTestId('tag-usage-1')
    await waitFor(() => expect(usageCell).toHaveTextContent('3 series, 42 episodes'))

    await user.type(screen.getByTestId('tag-new-label'), 'kids')
    await user.type(screen.getByTestId('tag-new-desc'), 'Kids TV')
    await user.click(screen.getByTestId('tags-add'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'tags',
        expect.objectContaining({
          tags: [expect.objectContaining({ label: 'family' }), expect.objectContaining({ label: 'kids' })],
        }),
      ),
    )
  })

  it('deletes a tag', async () => {
    const user = userEvent.setup()
    renderSection(<TagsSection />)
    await screen.findByTestId('tag-usage-1')
    await user.click(screen.getByTestId('tag-del-1'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'tags',
        expect.objectContaining({ tags: [] }),
      ),
    )
  })
})

describe('MediaManagementSection', () => {
  it('renders templates + mappings, edits one, and saves', async () => {
    const user = userEvent.setup()
    renderSection(<MediaManagementSection />)
    const episode = await screen.findByTestId('mm-episode')
    // The document loads async; wait for the seeded template to apply.
    await waitFor(() => expect(episode).toHaveValue('Episode {s}-{e} {q}'))
    // The seeded mapping row renders.
    expect(await screen.findByTestId('mm-mappings')).toHaveTextContent('/srv/torrents')

    // userEvent.type mangles '{s}'-style template tokens (key-sequence
    // parsing), so set the full template via a direct change event.
    fireEvent.change(episode, { target: { value: 'S{s}E{e}' } })
    await user.click(screen.getByTestId('media-management-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'media-management',
        expect.objectContaining({
          naming: expect.objectContaining({ episode: 'S{s}E{e}' }),
          remote_path_mappings: [{ source: '/srv/torrents', destination: '/data/torrents' }],
        }),
      ),
    )
  })

  it('adds a remote path mapping', async () => {
    const user = userEvent.setup()
    renderSection(<MediaManagementSection />)
    await screen.findByTestId('mm-episode')
    await user.type(screen.getByTestId('mm-map-source'), '/tmp/in')
    await user.type(screen.getByTestId('mm-map-dest'), '/data/in')
    await user.click(screen.getByTestId('mm-map-add'))
    expect(await screen.findByTestId('mm-mappings')).toHaveTextContent('/tmp/in')
  })
})
