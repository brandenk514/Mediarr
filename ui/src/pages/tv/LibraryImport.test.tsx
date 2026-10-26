import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { ImportReport } from '../../api/types'
import { LibraryImportPage } from './LibraryImport'

// Page test for the P2 Library Import wizard (phase 1): choose a root folder
// (registered or unmapped path) → scan → match/confirm → report.
vi.mock('../../api/client', () => ({
  client: {
    listRootFolders: vi.fn(),
    importLibrary: vi.fn(),
  },
}))

function renderImport() {
  return render(
    <MemoryRouter>
      <LibraryImportPage />
    </MemoryRouter>,
  )
}

const report: ImportReport = {
  Root: '/media/tv',
  Scanned: 2,
  Candidates: [
    { Path: '/media/tv/Breaking Bad (2008)', Title: 'Breaking Bad', Year: 2008, Seasons: 2, Files: 62, Size: 2040109440, Matched: true, SeriesID: 1 },
    { Path: '/media/tv/Some Other Show', Title: 'Some Other Show', Year: 0, Seasons: 1, Files: 3, Size: 31457280, Matched: false, SeriesID: 0 },
  ],
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.listRootFolders).mockResolvedValue([
    { ID: 7, Path: '/media/tv', Name: 'TV', CreatedAt: '', FreeSpace: null },
  ])
  vi.mocked(client.importLibrary).mockResolvedValue(report)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('LibraryImportPage (P2 wizard)', () => {
  it('step 1 lists registered root folders and scans the selected one', async () => {
    const user = userEvent.setup()
    renderImport()
    const select = await screen.findByTestId('import-root-select')
    expect(select).toHaveValue('7')

    await user.click(screen.getByTestId('import-scan'))
    await screen.findByTestId('import-results')
    expect(client.importLibrary).toHaveBeenCalledWith({ root_folder_id: 7 })
  })

  it('preselects matched candidates, leaves unmatched rows disabled', async () => {
    const user = userEvent.setup()
    renderImport()
    await screen.findByTestId('import-root-select')
    await user.click(screen.getByTestId('import-scan'))
    const results = await screen.findByTestId('import-results')

    const matched = within(results).getByTestId('import-pick-/media/tv/Breaking Bad (2008)') as HTMLInputElement
    expect(matched).toBeChecked()
    expect(matched).toBeEnabled()

    const unmatched = within(results).getByTestId('import-pick-/media/tv/Some Other Show') as HTMLInputElement
    expect(unmatched).not.toBeChecked()
    expect(unmatched).toBeDisabled()

    expect(screen.getByTestId('import-matched')).toHaveTextContent('1')
    expect(screen.getByTestId('import-scanned')).toHaveTextContent('2')
  })

  it('supports an unmapped free path', async () => {
    const user = userEvent.setup()
    renderImport()
    await screen.findByTestId('import-mode-path')
    await user.click(screen.getByTestId('import-mode-path'))
    await user.type(screen.getByTestId('import-path-input'), '/data/series')
    await user.click(screen.getByTestId('import-scan'))
    await screen.findByTestId('import-results')
    expect(client.importLibrary).toHaveBeenCalledWith({ path: '/data/series' })
  })

  it('advances scan → confirm → report and shows the summary', async () => {
    const user = userEvent.setup()
    renderImport()
    await screen.findByTestId('import-root-select')
    await user.click(screen.getByTestId('import-scan'))
    await screen.findByTestId('import-results')

    await user.click(screen.getByTestId('import-confirm'))
    const doneStep = await screen.findByTestId('import-step-report')
    expect(doneStep).toHaveClass('active')
    expect(screen.getByTestId('report-scanned')).toHaveTextContent('2')
    expect(screen.getByTestId('report-matched')).toHaveTextContent('1')
    expect(screen.getByTestId('report-selected')).toHaveTextContent('1')
  })

  it('keeps scan disabled until a root is chosen', async () => {
    vi.mocked(client.listRootFolders).mockResolvedValue([])
    renderImport()
    await screen.findByTestId('import-no-roots')
    expect(screen.getByTestId('import-scan')).toBeDisabled()
    // No root folders → the unmapped path is the way in.
    const input = screen.getByTestId('import-path-input')
    expect(input).toBeEnabled()
  })
})
