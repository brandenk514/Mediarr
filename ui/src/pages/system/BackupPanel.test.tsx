import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { BackupDocument, BackupImportResult } from '../../api/types'
import { BackupPanel } from './BackupPanel'

vi.mock('../../api/client', () => ({
  ApiError: class extends Error {},
  client: {
    getSystemBackup: vi.fn(),
    putSystemBackup: vi.fn(),
  },
}))

const DOC: BackupDocument = {
  version: '0.6.0',
  exported_at: '2026-10-25T00:00:00Z',
  settings: {
    download_client: {
      kind: 'qbittorrent',
      qb_base: 'http://localhost:8080',
      qb_username: 'admin',
      qb_api_key_set: true,
      qb_password_set: false,
      sab_base: '',
      sab_api_key_set: false,
      sab_complete_dir: '',
    },
    libraries: {},
    sections: [
      { section: 'general', document: '{"instance_name":"x"}' },
      { section: 'tags', document: '[]' },
    ],
  },
  queue: null,
  series: null,
}

const IMPORT_RESULT: BackupImportResult = {
  imported: true,
  settings_imported: 2,
  queue_imported: 3,
  series_imported: 1,
  message: 'restored from backup',
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSystemBackup as ReturnType<typeof vi.fn>).mockResolvedValue(DOC)
})

describe('BackupPanel', () => {
  it('renders the current-document summary (sections + download client)', async () => {
    render(<BackupPanel />)
    const summary = await screen.findByTestId('backup-summary')
    expect(summary).toHaveTextContent('2')
    expect(summary).toHaveTextContent('qbittorrent')
    expect(screen.getByTestId('system-backup')).toBeInTheDocument()
  })

  it('exports with the snapshot toggles it passes to the client', async () => {
    const user = userEvent.setup()
    render(<BackupPanel />)
    await screen.findByTestId('backup-summary')

    await user.click(screen.getByTestId('backup-include-queue'))
    await user.click(screen.getByTestId('backup-export-btn'))
    await vi.waitFor(() =>
      expect(client.getSystemBackup).toHaveBeenLastCalledWith({
        includeQueue: true,
        includeSeries: false,
      }),
    )
    const result = await screen.findByTestId('backup-export-result')
    expect(result).toHaveTextContent('"queue": null')
    expect(screen.getByTestId('backup-download-btn')).toBeInTheDocument()
  })

  it('imports a pasted document with the queue snapshot applied', async () => {
    const user = userEvent.setup()
    const put = vi.fn().mockResolvedValue(IMPORT_RESULT)
    ;(client.putSystemBackup as ReturnType<typeof vi.fn>).mockImplementation(put)
    render(<BackupPanel />)
    await screen.findByTestId('backup-import-text')

    // fireEvent.change is deterministic for a JSON document (userEvent's
    // keyboard parser chokes on the leading '{').
    fireEvent.change(screen.getByTestId('backup-import-text'), {
      target: { value: JSON.stringify(DOC) },
    })
    await user.click(screen.getByTestId('backup-import-queue'))
    await user.click(screen.getByTestId('backup-import-btn'))

    await vi.waitFor(() =>
      expect(put).toHaveBeenCalledWith({
        document: DOC,
        include_queue: true,
        include_series: false,
      }),
    )
    expect(await screen.findByTestId('backup-import-result')).toHaveTextContent(
      'Imported 2 settings sections',
    )
    expect(screen.getByTestId('backup-import-result')).toHaveTextContent('3 queue items')
  })

  it('rejects invalid JSON without calling the import endpoint', async () => {
    const user = userEvent.setup()
    render(<BackupPanel />)
    await screen.findByTestId('backup-import-text')

    fireEvent.change(screen.getByTestId('backup-import-text'), {
      target: { value: '{not json' },
    })
    await user.click(screen.getByTestId('backup-import-btn'))
    expect(screen.getByText('The pasted text is not valid JSON')).toBeInTheDocument()
    expect(client.putSystemBackup).not.toHaveBeenCalled()
  })

  it('shows the error-gated state when the backup endpoint fails (P1 dev stack)', async () => {
    ;(client.getSystemBackup as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Not Found'))
    render(<BackupPanel />)
    await screen.findByTestId('system-backup-error')
    expect(screen.getByText('Backup unavailable')).toBeInTheDocument()
  })
})
