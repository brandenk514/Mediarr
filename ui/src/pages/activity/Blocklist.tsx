import { Toolbar, TbButton, EmptyState } from '../../components/ui'

// Blocklist is the blocked-releases list at /activity/blocklist (route
// established in P1). The list + unblock + reason ship in P3; this is the
// scaffold a Sonarr user lands on at the right path.
export function BlocklistPage() {
  return (
    <div className="list-page" data-testid="blocklist">
      <div className="page-head">
        <h2>Blocklist</h2>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh" testId="blocklist-refresh" disabled />
            <TbButton icon="🗑" label="Delete Selected" testId="blocklist-delete" disabled divider />
          </>
        }
      />
      <EmptyState
        title="Queue is empty"
        message="Blocked releases appear here. The blocklist (list, unblock, reason) lands with the Activity/Wanted build (P3)."
        testId="blocklist-empty"
      />
    </div>
  )
}
