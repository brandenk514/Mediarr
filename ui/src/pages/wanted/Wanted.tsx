import { NavLink } from 'react-router-dom'
import { Toolbar, TbButton, EmptyState } from '../../components/ui'

type WantedTab = 'missing' | 'cutoff'

// Wanted is the Missing / Cutoff Unmet page (route established in P1). P1
// establishes the tabs + toolbar; the server-side lists, Search All, Unmonitor
// Selected, and Manual Import ship in P3.
export function WantedPage({ tab }: { tab: WantedTab }) {
  const isMissing = tab === 'missing'
  return (
    <div className="list-page" data-testid={`wanted-${tab}`}>
      <div className="page-head">
        <h2>Wanted</h2>
      </div>
      <div className="segmented" role="tablist" aria-label="Wanted" style={{ marginBottom: '1rem' }}>
        <NavLink to="/wanted/missing" role="tab" aria-selected={isMissing} className={`seg ${isMissing ? 'active' : ''}`} data-testid="wanted-tab-missing">
          Missing
        </NavLink>
        <NavLink to="/wanted/cutoff" role="tab" aria-selected={!isMissing} className={`seg ${!isMissing ? 'active' : ''}`} data-testid="wanted-tab-cutoff">
          Cutoff Unmet
        </NavLink>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="🔍" label="Search All" testId={`wanted-search-${tab}`} disabled />
            <TbButton icon="👁" label="Unmonitor Selected" testId={`wanted-unmonitor-${tab}`} disabled divider />
            <TbButton icon="📥" label="Manual Import" testId={`wanted-manual-${tab}`} disabled />
          </>
        }
      />
      <EmptyState
        title={isMissing ? 'No missing episodes' : 'No cutoff-unmet episodes'}
        message={
          isMissing
            ? 'Missing monitored episodes appear here. The Wanted list (search, unmonitor, manual import) lands with the Activity/Wanted build (P3).'
            : 'Episodes below their quality profile’s cutoff appear here. Cutoff unmet lands with the Activity/Wanted build (P3).'
        }
        testId={`wanted-empty-${tab}`}
      />
    </div>
  )
}
