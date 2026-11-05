import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { MigrationPanel } from './MigrationPanel'

vi.mock('../../api/client', () => ({
  ApiError: class extends Error {},
  client: {
    getMigrationStatus: vi.fn(),
    runMigration: vi.fn(),
    // B6: the dedicated Readarr endpoints.
    runReadarrMigration: vi.fn(),
    executeReadarrMigration: vi.fn(),
    getReadarrMigrationStatus: vi.fn(),
  },
}))

const STATUS = {
  status: 'idle',
  running: false,
  started_at: null,
  finished_at: null,
  done: 0,
  total: 0,
  last_result: null,
}

const ALL_INCLUDE = {
  series: true,
  episodes: true,
  quality_profiles: true,
  tags: true,
  indexers: true,
  download_client: true,
  root_folders: true,
  indexer_proxies: true,
}

const RESULT = {
  dry_run: true,
  resume: false,
  source_url: 'http://sonarr.local:8989',
  counts: { total: 4, created: 0, existing: 1, skipped: 0, errors: 0 },
  items: [
    { entity: 'series', external_id: '42', source_title: 'Severance', action: 'dry-run' },
    { entity: 'quality_profile', external_id: '1', source_title: 'HD-1080p', action: 'dry-run' },
    { entity: 'indexer', external_id: '9', source_title: 'NZBGeek', action: 'dry-run' },
    { entity: 'download_client', external_id: '1', source_title: 'qBittorrent', action: 'dry-run' },
  ],
  resumable: false,
}

// B6 fixtures mirroring the Readarr v0.4 drop-in shapes (the fake-server
// data from the wave-1 client tests).
const READARR_RESULT = {
  dry_run: true,
  resume: false,
  source_url: 'http://readarr.local:8888',
  counts: { total: 15, created: 0, existing: 0, skipped: 0, errors: 0 },
  items: [
    { entity: 'quality_profile', external_id: '3', source_title: 'Digital Unlimited', action: 'dry-run' },
    { entity: 'metadata_profile', external_id: '2', source_title: 'Books', action: 'dry-run' },
    { entity: 'tag', external_id: '4', source_title: 'Favorites', action: 'dry-run' },
    { entity: 'indexer', external_id: '9', source_title: 'SomeNZB', action: 'dry-run' },
    { entity: 'download_client', external_id: '9', source_title: 'SickChill', action: 'dry-run' },
    { entity: 'root_folder', external_id: '1', source_title: '/media/books', action: 'dry-run' },
    { entity: 'author', external_id: '7', source_title: 'Ursula K. Le Guin', action: 'dry-run' },
    { entity: 'book', external_id: '21', source_title: 'The Dispossessed', action: 'dry-run' },
    { entity: 'edition', external_id: '91', source_title: 'The Dispossessed: An Ambiguous Utopia', action: 'dry-run' },
  ],
  resumable: false,
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  ;(client.getMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue(STATUS)
  ;(client.runMigration as ReturnType<typeof vi.fn>).mockResolvedValue(RESULT)
  ;(client.getReadarrMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue(STATUS)
  ;(client.runReadarrMigration as ReturnType<typeof vi.fn>).mockResolvedValue(READARR_RESULT)
  ;(client.executeReadarrMigration as ReturnType<typeof vi.fn>).mockResolvedValue({
    ...READARR_RESULT,
    dry_run: false,
  })
})

describe('MigrationPanel', () => {
  it('renders the source form with dry-run on by default and all entities included', async () => {
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    expect(screen.getByTestId('migration-api-key')).toBeInTheDocument()
    const dryRun = screen.getByTestId('migration-dry-run') as HTMLInputElement
    expect(dryRun.checked).toBe(true)
    const resume = screen.getByTestId('migration-resume') as HTMLInputElement
    expect(resume.checked).toBe(false)
    for (const key of [
      'series',
      'episodes',
      'quality_profiles',
      'tags',
      'indexers',
      'download_client',
    ]) {
      expect((screen.getByTestId(`migration-include-${key}`) as HTMLInputElement).checked).toBe(
        true,
      )
    }
    // The status card is rendered alongside the form.
    expect(screen.getByTestId('migration-status')).toHaveTextContent('idle')
    // The run button is disabled until a source URL is entered.
    expect((screen.getByTestId('migration-run-btn') as HTMLButtonElement).disabled).toBe(true)
    // R6: the source toggle is present, defaulting to Sonarr.
    const source = screen.getByTestId('migration-source') as HTMLSelectElement
    expect(source.value).toBe('sonarr')
    expect(screen.getByTestId('migration-source-help')).toHaveTextContent('Base URL of the Sonarr instance.')
  })

  it('submits a dry run and renders the mapping report', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.type(screen.getByTestId('migration-source-url'), 'http://sonarr.local:8989')
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runMigration).toHaveBeenCalledWith({
        source_url: 'http://sonarr.local:8989',
        source: 'sonarr',
        dry_run: true,
        include: ALL_INCLUDE,
      }),
    )
    const report = await screen.findByTestId('migration-result')
    expect(report).toHaveTextContent('dry run')
    expect(screen.getByTestId('migration-counts')).toHaveTextContent('total')
    expect(screen.getByTestId('migration-item-0')).toHaveTextContent('Severance')
    expect(screen.getByTestId('migration-item-0')).toHaveTextContent('dry-run')
  })

  it('toggling dry-run off submits a real run', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.type(screen.getByTestId('migration-source-url'), 'http://sonarr.local:8989')
    await user.click(screen.getByTestId('migration-dry-run'))
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runMigration).toHaveBeenCalledWith({
        source_url: 'http://sonarr.local:8989',
        source: 'sonarr',
        dry_run: false,
        include: ALL_INCLUDE,
      }),
    )
  })

  it('sends resume + api key and narrowed includes when requested', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.type(screen.getByTestId('migration-source-url'), 'http://sonarr.local:8989')
    await user.type(screen.getByTestId('migration-api-key'), 'sekrit')
    await user.click(screen.getByTestId('migration-resume'))
    await user.click(screen.getByTestId('migration-include-episodes'))
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runMigration).toHaveBeenCalledWith({
        source_url: 'http://sonarr.local:8989',
        source: 'sonarr',
        api_key: 'sekrit',
        dry_run: true,
        resume: true,
        include: { ...ALL_INCLUDE, episodes: false },
      }),
    )
  })

  it('switching the source to Radarr changes the helper text and the payload (R6)', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    // Default: the Sonarr helper text.
    expect(screen.getByTestId('migration-source-help')).toHaveTextContent('Base URL of the Sonarr instance.')
    await user.selectOptions(screen.getByTestId('migration-source'), 'radarr')
    expect(screen.getByTestId('migration-source-help')).toHaveTextContent('Base URL of the Radarr instance.')

    await user.type(screen.getByTestId('migration-source-url'), 'https://radarr.local:7878')
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runMigration).toHaveBeenCalledWith({
        source_url: 'https://radarr.local:7878',
        source: 'radarr',
        dry_run: true,
        include: ALL_INCLUDE,
      }),
    )
  })

  it('switching the source to Prowlarr shows its labels and only its four entity classes (PR6)', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.selectOptions(screen.getByTestId('migration-source'), 'prowlarr')
    expect(screen.getByTestId('migration-source-help')).toHaveTextContent('Base URL of the Prowlarr instance.')
    // The base-URL placeholder and API-key label follow the source.
    expect(screen.getByTestId('migration-source-url')).toHaveAttribute('placeholder', 'https://prowlarr.local:9696')
    expect(screen.getByText('Prowlarr base URL')).toBeInTheDocument()
    expect(screen.getByText('Prowlarr API key')).toBeInTheDocument()
    // Exactly four include checkboxes: indexers, download clients, tags,
    // indexer proxies. No root folders, no series/episodes/profiles, no apps.
    for (const key of ['indexers', 'download_client', 'tags', 'indexer_proxies']) {
      expect(screen.getByTestId(`migration-include-${key}`)).toBeInTheDocument()
    }
    for (const key of ['series', 'episodes', 'quality_profiles', 'root_folders']) {
      expect(screen.queryByTestId(`migration-include-${key}`)).not.toBeInTheDocument()
    }
    // The summary text states the Prowlarr Apps section is not imported.
    expect(
      screen.getByText((t) => t.includes('Prowlarr Apps section is not imported')),
    ).toBeInTheDocument()

    await user.type(screen.getByTestId('migration-source-url'), 'https://prowlarr.local:9696')
    await user.type(screen.getByTestId('migration-api-key'), 'prowlarr-key')
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runMigration).toHaveBeenCalledWith({
        source_url: 'https://prowlarr.local:9696',
        source: 'prowlarr',
        api_key: 'prowlarr-key',
        dry_run: true,
        include: ALL_INCLUDE,
      }),
    )
  })

  it('flags a resumable partial run', async () => {
    ;(client.runMigration as ReturnType<typeof vi.fn>).mockResolvedValue({
      ...RESULT,
      dry_run: false,
      resumable: true,
    })
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.type(screen.getByTestId('migration-source-url'), 'http://sonarr.local:8989')
    await user.click(screen.getByTestId('migration-dry-run'))
    await user.click(screen.getByTestId('migration-run-btn'))

    expect(await screen.findByTestId('migration-resumable')).toHaveTextContent('partial')
  })

  it('surfaces run errors from the endpoint', async () => {
    ;(client.runMigration as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('sonarr unreachable: connection refused'),
    )
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.type(screen.getByTestId('migration-source-url'), 'http://sonarr.local:8989')
    await user.click(screen.getByTestId('migration-run-btn'))
    expect(await screen.findByText('sonarr unreachable: connection refused')).toBeInTheDocument()
    expect(screen.queryByTestId('migration-result')).not.toBeInTheDocument()
  })

  it('shows the error-gated state when the status endpoint fails (P1 dev stack)', async () => {
    ;(client.getMigrationStatus as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('Not Found'),
    )
    render(<MigrationPanel />)
    await screen.findByTestId('system-migration-error')
    expect(screen.getByText('Migration unavailable')).toBeInTheDocument()
  })
})

// ---- B6: the Readarr drop-in (dedicated endpoints, source-scoped status) ----

describe('MigrationPanel Readarr (B6)', () => {
  it('shows the Readarr copy, placeholder, and its seven include classes (no proxies)', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    // The readarr-scoped status endpoint is used for the status card.
    expect(client.getReadarrMigrationStatus).toHaveBeenCalled()
    // Copy + labels + placeholder follow the source.
    expect(screen.getByTestId('migration-source-help')).toHaveTextContent('Base URL of the Readarr instance.')
    expect(screen.getByTestId('migration-source-url')).toHaveAttribute('placeholder', 'http://readarr.local:8888')
    expect(screen.getByText('Readarr base URL')).toBeInTheDocument()
    expect(screen.getByText('Readarr API key')).toBeInTheDocument()
    // The shared include keys re-label onto Readarr's entity classes.
    expect(screen.getByText('authors + books')).toBeInTheDocument()
    expect(screen.getByText('editions + book files')).toBeInTheDocument()
    expect(screen.getByText('quality + metadata profiles')).toBeInTheDocument()
    expect(screen.getByText('root folders (Calibre block)')).toBeInTheDocument()
    // Seven include classes — root folders included (Calibre block), no
    // indexer proxies.
    for (const key of ['series', 'episodes', 'quality_profiles', 'tags', 'indexers', 'download_client', 'root_folders']) {
      expect(screen.getByTestId(`migration-include-${key}`)).toBeInTheDocument()
    }
    expect(screen.queryByTestId('migration-include-indexer_proxies')).not.toBeInTheDocument()
    // The done/total noun is books.
    expect(screen.getByTestId('migration-status')).toHaveTextContent('0/0 books')
  })

  it('keeps the run button disabled until base_url is entered (form validation)', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    expect((screen.getByTestId('migration-run-btn') as HTMLButtonElement).disabled).toBe(true)
    await user.type(screen.getByTestId('migration-source-url'), 'http://readarr.local:8888')
    expect((screen.getByTestId('migration-run-btn') as HTMLButtonElement).disabled).toBe(false)
  })

  it('dry run posts to the dedicated readarr endpoint and renders the mapping report', async () => {
    const user = userEvent.setup()
    // After the run the panel refreshes the source-scoped status.
    ;(client.getReadarrMigrationStatus as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(STATUS)
      .mockResolvedValueOnce({ ...STATUS, status: 'completed', done: 1, total: 1 })
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    await user.type(screen.getByTestId('migration-source-url'), 'http://readarr.local:8888')
    await user.type(screen.getByTestId('migration-api-key'), 'readarr-key')
    await user.click(screen.getByTestId('migration-run-btn'))

    await vi.waitFor(() =>
      expect(client.runReadarrMigration).toHaveBeenCalledWith({
        base_url: 'http://readarr.local:8888',
        api_key: 'readarr-key',
        include: ALL_INCLUDE,
      }),
    )
    // The generic endpoint and the execute endpoint stay untouched.
    expect(client.runMigration).not.toHaveBeenCalled()
    expect(client.executeReadarrMigration).not.toHaveBeenCalled()
    // The report renders with counts + item rows (actions all 'dry-run').
    const report = await screen.findByTestId('migration-result')
    expect(report).toHaveTextContent('dry run')
    const counts = screen.getByTestId('migration-counts')
    expect(counts).toHaveTextContent('total')
    expect(counts).toHaveTextContent('15')
    expect(screen.getByTestId('migration-item-0')).toHaveTextContent('Digital Unlimited')
    expect(screen.getByTestId('migration-item-7')).toHaveTextContent('The Dispossessed')
    expect(screen.getByTestId('migration-item-7')).toHaveTextContent('dry-run')
    // The dry run never reaches the execute endpoint, and the status card
    // settles on the readarr-scoped completed state.
    await vi.waitFor(() => expect(screen.getByTestId('migration-status')).toHaveTextContent('completed'))
    expect(screen.getByTestId('migration-status')).toHaveTextContent('1/1 books')
  })

  it('execute confirms first, then posts to the execute endpoint with resume', async () => {
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    await user.type(screen.getByTestId('migration-source-url'), 'http://readarr.local:8888')
    await user.click(screen.getByTestId('migration-dry-run')) // off -> real run
    await user.click(screen.getByTestId('migration-resume')) // resume a partial run
    await user.click(screen.getByTestId('migration-run-btn'))

    expect(window.confirm).toHaveBeenCalledTimes(1)
    await vi.waitFor(() =>
      expect(client.executeReadarrMigration).toHaveBeenCalledWith({
        base_url: 'http://readarr.local:8888',
        include: ALL_INCLUDE,
        resume: true,
      }),
    )
    expect(client.runReadarrMigration).not.toHaveBeenCalled()
  })

  it('execute honours a declined confirm and calls nothing', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')

    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    await user.type(screen.getByTestId('migration-source-url'), 'http://readarr.local:8888')
    await user.click(screen.getByTestId('migration-dry-run')) // off -> real run
    await user.click(screen.getByTestId('migration-run-btn'))

    expect(window.confirm).toHaveBeenCalledTimes(1)
    expect(client.executeReadarrMigration).not.toHaveBeenCalled()
    expect(client.runReadarrMigration).not.toHaveBeenCalled()
    expect(screen.queryByTestId('migration-result')).not.toBeInTheDocument()
  })

  it('shows running progress with the books noun', async () => {
    ;(client.getReadarrMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      status: 'running',
      running: true,
      started_at: '2026-11-04T00:00:00Z',
      finished_at: null,
      done: 3,
      total: 10,
      last_result: null,
    })
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    const status = await screen.findByTestId('migration-status')
    expect(status).toHaveTextContent('running')
    expect(status).toHaveTextContent('3/10 books')
  })

  it('shows a partial run as resumable with the resume affordance', async () => {
    ;(client.getReadarrMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      status: 'partial',
      running: false,
      started_at: null,
      finished_at: '2026-11-04T00:00:00Z',
      done: 2,
      total: 5,
      last_result: {
        ...READARR_RESULT,
        dry_run: false,
        resumable: true,
        items: READARR_RESULT.items.slice(0, 2),
        counts: { total: 2, created: 2, existing: 0, skipped: 0, errors: 0 },
      },
    })
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    expect(await screen.findByTestId('migration-status')).toHaveTextContent('partial')
    expect(await screen.findByTestId('migration-resumable')).toHaveTextContent('partial')
    // The resume checkbox is the resume affordance.
    expect((screen.getByTestId('migration-resume') as HTMLInputElement).checked).toBe(false)
  })

  it('shows the empty-report state when the source is unreachable (200-empty + failed)', async () => {
    ;(client.getReadarrMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      status: 'failed',
      running: false,
      started_at: null,
      finished_at: '2026-11-04T00:00:00Z',
      done: 0,
      total: 0,
      last_result: {
        dry_run: true,
        resume: false,
        counts: { total: 0, created: 0, existing: 0, skipped: 0, errors: 0 },
        items: [],
        resumable: false,
      },
    })
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    expect(await screen.findByTestId('migration-status')).toHaveTextContent('failed')
    expect(await screen.findByTestId('migration-failed')).toHaveTextContent('could not be reached')
    // The 200-empty report renders its empty state (no item rows).
    expect(await screen.findByTestId('migration-empty-report')).toHaveTextContent('No entities were mapped')
    expect(screen.queryByTestId('migration-item-0')).not.toBeInTheDocument()
  })

  it('surfaces 400 validation errors from the dedicated endpoints', async () => {
    ;(client.runReadarrMigration as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('base_url is required'),
    )
    const user = userEvent.setup()
    render(<MigrationPanel />)
    await screen.findByTestId('migration-source-url')
    await user.selectOptions(screen.getByTestId('migration-source'), 'readarr')
    await user.type(screen.getByTestId('migration-source-url'), 'http://readarr.local:8888')
    await user.click(screen.getByTestId('migration-run-btn'))
    expect(await screen.findByText('base_url is required')).toBeInTheDocument()
  })
})
