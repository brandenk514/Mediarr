import { Toolbar, TbButton, EmptyState } from '../../components/ui'

// LibraryImport is the Library Import wizard at /tv/import (route established
// in P1). The wizard (root folders → scan → match → confirm → report) ships in
// P2; this is the scaffold a Sonarr user lands on at the right path.
export function LibraryImportPage() {
  return (
    <div className="list-page" data-testid="library-import">
      <div className="page-head">
        <h2>Library Import</h2>
      </div>
      <Toolbar
        left={<TbButton icon="📁" label="Add Folder" testId="import-add-folder" disabled />}
      />
      <EmptyState
        title="No root folders yet"
        message="Add a root folder to scan for series. The import wizard (scan → match → confirm → report) lands with the TV parity build (P2)."
        testId="library-import-empty"
      />
    </div>
  )
}
