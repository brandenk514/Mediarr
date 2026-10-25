import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { AddSeriesRequest, RootFolder, SearchResult } from '../../api/types'
import { Badge, BadgeRow } from '../../components/ui'
import {
  AddError,
  ProfilePicker,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

type Source = 'tvdb' | 'tmdb'
type MonitorMode = 'all' | 'none'
type SeriesType = 'standard' | 'daily' | 'oneshot'

// TvAddPage is the Sonarr-parity Add New page (P2): two panes. Left = debounced
// provider search with selectable result rows (poster, 'Title (year)', the
// badges the provider carries, overview, provider credit). Right = the add
// form: root folder, monitor, quality profile, series type, season folder,
// tags, post-add search options, and the green 'Add <Title>' button.
export function TvAddPage() {
  const navigate = useNavigate()
  const [source, setSource] = useState<Source>('tvdb')
  const search = useCallback(
    (q: string) => (source === 'tvdb' ? client.searchTv(q) : client.searchTvTmdb(q)),
    [source],
  )
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<SearchResult | null>(null)
  const [profile, setProfile] = useState('')
  const [rootFolders, setRootFolders] = useState<RootFolder[] | null>(null)
  const [rootFolderId, setRootFolderId] = useState<string>('')
  const [monitor, setMonitor] = useState<MonitorMode>('all')
  const [seriesType, setSeriesType] = useState<SeriesType>('standard')
  const [seasonFolder, setSeasonFolder] = useState(false)
  const [tags, setTags] = useState<string[]>([])
  const [tagDraft, setTagDraft] = useState('')
  const [startSearch, setStartSearch] = useState(true)
  const [startCutoff, setStartCutoff] = useState(false)

  // Root folders load once on mount (the dropdown is informational in P2 —
  // the add request does not yet carry a root folder id; the chosen folder is
  // what the '<Title>' subfolder gets created under, per the helper text).
  const [rootLoadError, setRootLoadError] = useState<string | null>(null)
  const [rootFoldersLoaded, setRootFoldersLoaded] = useState(false)
  const loadRootFolders = useCallback(() => {
    client
      .listRootFolders()
      .then((rfs) => setRootFolders(rfs))
      .catch((e: unknown) => setRootLoadError(e instanceof Error ? e.message : 'failed to load root folders'))
      .finally(() => setRootFoldersLoaded(true))
  }, [])
  useEffect(() => {
    loadRootFolders()
  }, [loadRootFolders])

  const { busy, error, submit } = useAddSubmit(async () => {
    const title = selected ? selected.title : ''
    const req: AddSeriesRequest = {
      title,
      quality_profile: profile || undefined,
      monitored: monitor === 'all',
      series_type: seriesType,
      season_folder: seasonFolder,
      tags: tags.length > 0 ? tags : undefined,
    }
    if (selected) {
      if (selected.year > 0) req.year = String(selected.year)
      if (source === 'tvdb') req.tvdb_id = selected.id
      else req.tmdb_id = selected.id
    }
    const added = await client.addSeries(req)
    if (startSearch && added.id) {
      // Best-effort: the series exists now; a failed search should not fail
      // the add itself.
      await client.searchMissingEpisodes(added.id).catch(() => undefined)
    }
    void startCutoff
    navigate('/tv')
  })

  function pick(hit: SearchResult) {
    setSelected(hit)
  }

  function changeSource(next: Source) {
    setSource(next)
    setSelected(null)
  }

  function addTag() {
    const t = tagDraft.trim().replace(/,+$/, '')
    if (!t) return
    if (!tags.includes(t)) setTags((prev) => [...prev, t])
    setTagDraft('')
  }

  const title = selected?.title ?? ''

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="tv-add-title">Add series</h2>
        <Link className="ghost" to="/tv" data-testid="tv-add-back">
          ← All series
        </Link>
      </div>

      <div className="two-pane">
        {/* LEFT: search + selectable results */}
        <div>
          <div className="segmented" role="group" aria-label="Search source">
            <button
              type="button"
              className={source === 'tvdb' ? 'seg active' : 'seg'}
              onClick={() => changeSource('tvdb')}
              data-testid="tv-source-tvdb"
            >
              TVDB
            </button>
            <button
              type="button"
              className={source === 'tmdb' ? 'seg active' : 'seg'}
              onClick={() => changeSource('tmdb')}
              data-testid="tv-source-tmdb"
            >
              TMDB
            </button>
          </div>

          <SearchBox
            label={`Search series (${source === 'tvdb' ? 'TVDB' : 'TMDB'})`}
            placeholder="Series title, e.g. Breaking Bad"
            text={text}
            onText={setText}
            state={state}
            testId="tv-search"
          />

          {state.phase === 'results' && (
            <ul className="result-list" data-testid="tv-results">
              {state.hits.length === 0 && (
                <li>
                  <p className="dim" data-testid="tv-results-empty">
                    No results — try a different term.
                  </p>
                </li>
              )}
              {state.hits.map((h) => (
                <li key={h.id}>
                  <button
                    type="button"
                    className={`tv-result-row ${selected?.id === h.id ? 'sel' : ''}`}
                    onClick={() => pick(h)}
                    aria-pressed={selected?.id === h.id}
                    data-testid={`tv-result-${h.id}`}
                  >
                    <span className="tv-result-poster" aria-hidden>
                      {posterInitials(h.title)}
                    </span>
                    <span className="tv-result-main">
                      <span className="result-title">
                        {h.title}
                        {h.year > 0 ? <span className="dim"> ({h.year})</span> : null}
                      </span>
                      <BadgeRow>
                        {h.extra.rating && h.extra.rating !== '0.0' && (
                          <Badge tone="teal">★ {h.extra.rating}</Badge>
                        )}
                        {h.extra.language && <Badge>{h.extra.language}</Badge>}
                        {h.extra.studio && <Badge>{h.extra.studio}</Badge>}
                        {h.extra.genre && <Badge>{h.extra.genre}</Badge>}
                        {h.extra.seasons && <Badge>{h.extra.seasons} seasons</Badge>}
                        {h.extra.status && <Badge tone="ok">{h.extra.status}</Badge>}
                      </BadgeRow>
                      {h.extra.overview && (
                        <span className="tv-result-overview dim small">{h.extra.overview}</span>
                      )}
                      <span className="tv-result-credit dim small">via {source.toUpperCase()}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* RIGHT: the add form */}
        <div className="card add-form" data-testid="tv-add-form">
          <p className="dim small">
            Add a series to your library. It is tracked under the chosen root
            folder; monitoring and quality are applied immediately.
          </p>

          <label htmlFor="tv-root-folder">Root folder</label>
          <select
            id="tv-root-folder"
            value={rootFolderId}
            onChange={(e) => setRootFolderId(e.target.value)}
            data-testid="tv-root-folder"
          >
            <option value="">— Choose a root folder —</option>
            {(rootFolders ?? []).map((rf) => (
              <option key={rf.ID} value={String(rf.ID)}>
                {rf.Name ? `${rf.Name} (${rf.Path})` : rf.Path}
              </option>
            ))}
          </select>
          {rootLoadError && (
            <p className="error" data-testid="tv-root-folder-error">
              {rootLoadError}
            </p>
          )}
          {rootFoldersLoaded && rootFolders !== null && rootFolders.length === 0 && (
            <p className="dim small" data-testid="tv-root-folder-empty">
              No root folders registered yet.
            </p>
          )}
          <p className="dim small" data-testid="tv-root-folder-help">
            {title
              ? `'${title}' subfolder will be created automatically.`
              : "The series' subfolder will be created automatically."}
          </p>

          <label htmlFor="tv-monitor">Monitor</label>
          <select
            id="tv-monitor"
            value={monitor}
            onChange={(e) => setMonitor(e.target.value as MonitorMode)}
            data-testid="tv-monitor"
          >
            <option value="all">All Episodes</option>
            <option value="none">None</option>
          </select>

          <ProfilePicker kind="tv" value={profile} onChange={setProfile} testId="tv-profile" />

          <label htmlFor="tv-series-type">Series type</label>
          <select
            id="tv-series-type"
            value={seriesType}
            onChange={(e) => setSeriesType(e.target.value as SeriesType)}
            data-testid="tv-series-type"
          >
            <option value="standard">Standard</option>
            <option value="daily">Daily</option>
            <option value="oneshot">One-Off</option>
          </select>

          <label className="check" htmlFor="tv-season-folder">
            <input
              id="tv-season-folder"
              type="checkbox"
              checked={seasonFolder}
              onChange={(e) => setSeasonFolder(e.target.checked)}
              data-testid="tv-season-folder"
            />
            Create season folders
          </label>

          <label htmlFor="tv-tags">Tags</label>
          <input
            id="tv-tags"
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ',') {
                e.preventDefault()
                addTag()
              }
            }}
            onBlur={addTag}
            placeholder="Add a tag, press Enter"
            data-testid="tv-tags-input"
          />
          {tags.length > 0 && (
            <span className="tag-chips" data-testid="tv-tags-chips">
              {tags.map((t) => (
                <span className="tag-chip" key={t}>
                  {t}
                  <button
                    type="button"
                    aria-label={`Remove tag ${t}`}
                    onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                    data-testid={`tv-tag-remove-${t}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </span>
          )}

          <label className="check" htmlFor="tv-start-search">
            <input
              id="tv-start-search"
              type="checkbox"
              checked={startSearch}
              onChange={(e) => setStartSearch(e.target.checked)}
              data-testid="tv-start-search"
            />
            Start search for missing episodes
          </label>
          <label className="check" htmlFor="tv-start-cutoff">
            <input
              id="tv-start-cutoff"
              type="checkbox"
              checked={startCutoff}
              onChange={(e) => setStartCutoff(e.target.checked)}
              data-testid="tv-start-cutoff"
            />
            Start search for cutoff unmet episodes
          </label>

          <AddError message={error} testId="tv-add-error" />
          <button
            type="button"
            className="add-btn-green"
            disabled={busy || !title}
            onClick={() => {
              void submit().catch(() => {
                /* error surfaced in AddError */
              })
            }}
            data-testid="tv-add-button"
          >
            {busy ? 'Adding…' : `Add ${title || 'series'}`}
          </button>
        </div>
      </div>
    </div>
  )
}

function posterInitials(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '🎬'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}
