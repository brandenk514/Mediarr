import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { UISettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// The UI section's default-view vocabulary (mirrors the server's enum).
const DEFAULT_VIEWS = ['dashboard', 'calendar', 'queue', 'wanted', 'series', 'movies', 'artists', 'authors']

// Lidarr L3: the UI themes the server accepts (mirrors settings.UIThemes).
const THEMES: UISettings['theme'][] = ['auto', 'dark', 'light']

// UISection is the interface-preferences section: the default landing view and
// the date/time formatting options, plus the Lidarr L3 music bits — the
// artist/album detail page's "Expand Items by Default" per release type, the
// UI theme, and color-impaired contrast.
export function UISection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<UISettings>('ui')

  const [defaultView, setDefaultView] = useState<UISettings['default_view']>('dashboard')
  const [dateFormat, setDateFormat] = useState('YYYY-MM-DD')
  const [timeFormat, setTimeFormat] = useState<'24h' | '12h'>('24h')
  const [itemsPerPage, setItemsPerPage] = useState('50')
  // Lidarr L3 music bits. The ?? defaults mirror the server's DefaultUI()
  // for documents that predate the fields (old saved docs arrive with the
  // keys absent, not false/'' — a missing key must seed the default, or the
  // shell's stale-fixture render would throw).
  const [expandAlbums, setExpandAlbums] = useState(true)
  const [expandEps, setExpandEps] = useState(false)
  const [expandSingles, setExpandSingles] = useState(false)
  const [expandBroadcast, setExpandBroadcast] = useState(false)
  const [expandOther, setExpandOther] = useState(false)
  const [theme, setTheme] = useState<UISettings['theme']>('auto')
  const [colorImpaired, setColorImpaired] = useState(false)
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setDefaultView(doc.default_view)
    setDateFormat(doc.date_format)
    setTimeFormat(doc.time_format)
    setItemsPerPage(String(doc.items_per_page))
    setExpandAlbums(doc.expand_albums ?? true)
    setExpandEps(doc.expand_eps ?? false)
    setExpandSingles(doc.expand_singles ?? false)
    setExpandBroadcast(doc.expand_broadcast ?? false)
    setExpandOther(doc.expand_other ?? false)
    setTheme(doc.theme ?? 'auto')
    setColorImpaired(doc.color_impaired ?? false)
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="ui-error" />
  }

  function handleSave() {
    if (!doc) return
    void submit(async () => {
      const next: UISettings = {
        default_view: defaultView,
        date_format: dateFormat,
        time_format: timeFormat,
        items_per_page: Number(itemsPerPage) || 0,
        expand_albums: expandAlbums,
        expand_eps: expandEps,
        expand_singles: expandSingles,
        expand_broadcast: expandBroadcast,
        expand_other: expandOther,
        theme,
        color_impaired: colorImpaired,
      }
      await client.putSettingsSection<UISettings>('ui', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard title="UI" hint="Interface preferences: where you land and how dates render." testId="ui-card">
      <label htmlFor="ui-view">Default view</label>
      <select id="ui-view" value={defaultView}        onChange={(e) => setDefaultView(e.target.value as UISettings['default_view'])} data-testid="ui-view">
        {DEFAULT_VIEWS.map((v) => (
          <option key={v} value={v}>
            {v}
          </option>
        ))}
      </select>
      <label htmlFor="ui-date">Date format</label>
      <input id="ui-date" value={dateFormat} onChange={(e) => setDateFormat(e.target.value)} data-testid="ui-date" />
      <label htmlFor="ui-time">Time format</label>
      <select
        id="ui-time"
        value={timeFormat}
        onChange={(e) => setTimeFormat(e.target.value as '24h' | '12h')}
        data-testid="ui-time"
      >
        <option value="24h">24h</option>
        <option value="12h">12h</option>
      </select>
      <label htmlFor="ui-items">Items per page</label>
      <input
        id="ui-items"
        type="number"
        value={itemsPerPage}
        onChange={(e) => setItemsPerPage(e.target.value)}
        data-testid="ui-items"
      />

      <h4>Expand Items by Default</h4>
      <p className="dim small">
        The artist/album detail page starts with these item types expanded.
      </p>
      <div data-testid="ui-expand">
        <label className="check" htmlFor="ui-expand-albums">
          <input
            id="ui-expand-albums"
            type="checkbox"
            checked={expandAlbums}
            onChange={(e) => setExpandAlbums(e.target.checked)}
            data-testid="ui-expand-albums"
          />
          Albums
        </label>
        <label className="check" htmlFor="ui-expand-eps">
          <input
            id="ui-expand-eps"
            type="checkbox"
            checked={expandEps}
            onChange={(e) => setExpandEps(e.target.checked)}
            data-testid="ui-expand-eps"
          />
          EPs
        </label>
        <label className="check" htmlFor="ui-expand-singles">
          <input
            id="ui-expand-singles"
            type="checkbox"
            checked={expandSingles}
            onChange={(e) => setExpandSingles(e.target.checked)}
            data-testid="ui-expand-singles"
          />
          Singles
        </label>
        <label className="check" htmlFor="ui-expand-broadcast">
          <input
            id="ui-expand-broadcast"
            type="checkbox"
            checked={expandBroadcast}
            onChange={(e) => setExpandBroadcast(e.target.checked)}
            data-testid="ui-expand-broadcast"
          />
          Broadcast
        </label>
        <label className="check" htmlFor="ui-expand-other">
          <input
            id="ui-expand-other"
            type="checkbox"
            checked={expandOther}
            onChange={(e) => setExpandOther(e.target.checked)}
            data-testid="ui-expand-other"
          />
          Other
        </label>
      </div>
      <label htmlFor="ui-theme">Theme</label>
      <select
        id="ui-theme"
        value={theme}
        onChange={(e) => setTheme(e.target.value as UISettings['theme'])}
        data-testid="ui-theme"
      >
        {THEMES.map((t) => (
          <option key={t} value={t}>
            {t}
          </option>
        ))}
      </select>
      <label className="check" htmlFor="ui-color-impaired">
        <input
          id="ui-color-impaired"
          type="checkbox"
          checked={colorImpaired}
          onChange={(e) => setColorImpaired(e.target.checked)}
          data-testid="ui-color-impaired"
        />
        Color-Impaired Mode
      </label>
      {saved && (
        <p className="pill pill-ok" data-testid="ui-saved">
          Saved
        </p>
      )}
      <button onClick={handleSave} disabled={busy || loading} data-testid="ui-save">
        {busy ? 'Saving…' : 'Save UI'}
      </button>
    </SectionCard>
  )
}
