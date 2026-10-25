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

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getMigrationStatus as ReturnType<typeof vi.fn>).mockResolvedValue(STATUS)
  ;(client.runMigration as ReturnType<typeof vi.fn>).mockResolvedValue(RESULT)
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
        api_key: 'sekrit',
        dry_run: true,
        resume: true,
        include: { ...ALL_INCLUDE, episodes: false },
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
