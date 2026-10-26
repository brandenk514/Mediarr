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
        // Radarr R5a: the doc carries the movie ladder (100+ id space) too.
        movie_levels: [
          { id: 100, name: '480p', source: 'dvd', resolution: '480p', size_limit_mb: 1024 },
          { id: 104, name: '1080p WEB', source: 'webrip', resolution: '1080p', size_limit_mb: 1536 },
          { id: 105, name: '1080p', source: 'blu-ray', resolution: '1080p', size_limit_mb: 4096 },
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
        // Radarr R5a: the seeded movie profiles (100+ id space; items
        // reference the movie level ids), plus the default delay and release
        // profiles.
        movie_profiles: [
          {
            id: 100,
            name: '4K Best Effort',
            cutoff: 105,
            items: [
              { quality: 100, approved: true },
              { quality: 104, approved: true },
              { quality: 105, approved: true },
            ],
          },
          {
            id: 101,
            name: '4K',
            cutoff: 105,
            items: [{ quality: 105, approved: true }],
          },
        ],
        delay_profiles: [
          {
            id: 1,
            name: 'Default',
            description: '',
            webrip: 0,
            bluray: 0,
            remux: 0,
            brdisk: 0,
            rawhd: 0,
            hdtv: 0,
          },
        ],
        release_profiles: [
          {
            id: 1,
            name: 'Default',
            description: '',
            webrip: true,
            bluray: true,
            remux: true,
            brdisk: true,
            rawhd: true,
            hdtv: true,
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
        // Radarr R5a: the Radarr movie-naming block.
        movie_naming: {
          rename_movies: true,
          replace_illegal_characters: true,
          colon_replacement: '',
          standard_format: '{movie-title} ({year}) {quality} {edition}',
        },
        file_management: {
          min_file_age_hours: 0,
          remove_empty_dirs: true,
          import_extra_files: false,
          rename_series: false,
          unmonitor_deleted_movies: true,
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
          // The movie ladder round-trips untouched when only TV levels change.
          movie_levels: [
            { id: 100, name: '480p', source: 'dvd', resolution: '480p', size_limit_mb: 1024 },
            { id: 104, name: '1080p WEB', source: 'webrip', resolution: '1080p', size_limit_mb: 1536 },
            { id: 105, name: '1080p', source: 'blu-ray', resolution: '1080p', size_limit_mb: 4096 },
          ],
        },
      ),
    )
  })

  it('renders the movie quality definitions and saves a renamed movie level (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<QualitySection />)
    // The Radarr movie ladder renders in its own table (100+ id space).
    expect(await screen.findByTestId('movie-quality-name-100')).toHaveValue('480p')
    expect(screen.getByTestId('movie-quality-name-105')).toHaveValue('1080p')
    expect(screen.getByTestId('movie-quality-source-104')).toHaveValue('webrip')
    expect(screen.getByTestId('movie-quality-size-104')).toHaveValue(1536)

    await user.clear(screen.getByTestId('movie-quality-name-100'))
    await user.type(screen.getByTestId('movie-quality-name-100'), '480p-R')
    await user.click(screen.getByTestId('quality-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'quality',
        expect.objectContaining({
          movie_levels: [
            expect.objectContaining({ id: 100, name: '480p-R' }),
            expect.objectContaining({ id: 104, name: '1080p WEB' }),
            expect.objectContaining({ id: 105, name: '1080p' }),
          ],
        }),
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

  it('lists the seeded movie quality profiles (R5b)', async () => {
    renderSection(<ProfilesSection />)
    // The seeded movie profiles render alongside the TV profiles.
    const mvName = await screen.findByTestId('mvprofile-name-100')
    expect(mvName).toHaveValue('4K Best Effort')
    expect(screen.getByTestId('mvprofile-name-101')).toHaveValue('4K')
    // The TV profile is untouched and still renders.
    expect(screen.getByTestId('profile-name-1')).toHaveValue('Default')
    // Movie items reference the 100+ movie level id space.
    const cutoff = screen.getByTestId('mvprofile-cutoff-100') as HTMLSelectElement
    expect(cutoff).toHaveValue('105')
    // The level checkboxes come from the quality document (second fetch).
    expect(await screen.findByTestId('mvprofile-level-100-104')).toBeChecked()
  })

  it('edits a movie profile and saves the round-trip (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<ProfilesSection />)
    await screen.findByTestId('mvprofile-100')
    // The level checkboxes/cutoff options come from the quality document
    // (a second fetch) — wait for one to exist before driving the form.
    await screen.findByTestId('mvprofile-level-101-104')
    // Rename '4K', move its cutoff to the 100 level, and approve 1080p.
    await user.clear(screen.getByTestId('mvprofile-name-101'))
    await user.type(screen.getByTestId('mvprofile-name-101'), '4K Pro')
    await user.selectOptions(screen.getByTestId('mvprofile-cutoff-101'), '100')
    await user.click(screen.getByTestId('mvprofile-level-101-104'))

    await user.click(screen.getByTestId('profiles-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'profiles',
        expect.objectContaining({
          movie_profiles: [
            expect.objectContaining({ id: 100, name: '4K Best Effort' }),
            expect.objectContaining({
              id: 101,
              name: '4K Pro',
              cutoff: 100,
              items: [
                expect.objectContaining({ quality: 105, approved: true }),
                expect.objectContaining({ quality: 104, approved: true }),
              ],
            }),
          ],
        }),
      ),
    )
  })

  it('edits delay-profile hours and saves (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<ProfilesSection />)
    await screen.findByTestId('delay-1')
    // Webrip delay 0 → 2 hours; the other sources stay at 0.
    fireEvent.change(screen.getByTestId('delay-hours-1-webrip'), { target: { value: '2' } })

    await user.click(screen.getByTestId('profiles-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'profiles',
        expect.objectContaining({
          delay_profiles: [
            expect.objectContaining({ id: 1, name: 'Default', webrip: 2, bluray: 0, hdtv: 0 }),
          ],
        }),
      ),
    )
  })

  it('toggles release-profile flags and saves (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<ProfilesSection />)
    await screen.findByTestId('rel-1')
    // Bluray starts approved; click it out.
    expect(screen.getByTestId('rel-flag-1-bluray')).toBeChecked()
    await user.click(screen.getByTestId('rel-flag-1-bluray'))

    await user.click(screen.getByTestId('profiles-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'profiles',
        expect.objectContaining({
          release_profiles: [
            expect.objectContaining({ id: 1, name: 'Default', bluray: false, webrip: true }),
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

  it('renders the Radarr movie naming block and saves its edits (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<MediaManagementSection />)
    const format = await screen.findByTestId('mm-movie-format')
    // The document loads async; wait for the seeded template to apply.
    await waitFor(() =>
      expect(format).toHaveValue('{movie-title} ({year}) {quality} {edition}'),
    )
    expect(screen.getByTestId('mm-movie-rename')).toBeChecked()
    expect(screen.getByTestId('mm-movie-illegal')).toBeChecked()
    expect(screen.getByTestId('mm-movie-colon')).toHaveValue('')

    // Uncheck Rename Movies, set a colon replacement, and swap the format.
    await user.click(screen.getByTestId('mm-movie-rename'))
    await user.type(screen.getByTestId('mm-movie-colon'), '’')
    // Braces break userEvent key-sequence parsing; set the template directly.
    fireEvent.change(format, {
      target: { value: '{movie-title} ({year}) {quality}' },
    })
    await user.click(screen.getByTestId('media-management-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'media-management',
        expect.objectContaining({
          movie_naming: {
            rename_movies: false,
            replace_illegal_characters: true,
            colon_replacement: '’',
            standard_format: '{movie-title} ({year}) {quality}',
          },
        }),
      ),
    )
  })

  it('toggles Unmonitor Deleted Movies in file management and saves (R5b)', async () => {
    const user = userEvent.setup()
    renderSection(<MediaManagementSection />)
    // The seeded default is on; click turns it off.
    expect(await screen.findByTestId('mm-unmonitor')).toBeChecked()

    await user.click(screen.getByTestId('mm-unmonitor'))
    await user.click(screen.getByTestId('media-management-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'media-management',
        expect.objectContaining({
          file_management: expect.objectContaining({ unmonitor_deleted_movies: false }),
        }),
      ),
    )
  })
})
