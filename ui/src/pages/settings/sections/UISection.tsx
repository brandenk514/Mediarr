import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { UISettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// The UI section's default-view vocabulary (mirrors the server's enum).
const DEFAULT_VIEWS = ['dashboard', 'calendar', 'queue', 'wanted', 'series', 'movies', 'artists', 'authors']

// UISection is the interface-preferences section: the default landing view and
// the date/time formatting options.
export function UISection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<UISettings>('ui')

  const [defaultView, setDefaultView] = useState<UISettings['default_view']>('dashboard')
  const [dateFormat, setDateFormat] = useState('YYYY-MM-DD')
  const [timeFormat, setTimeFormat] = useState<'24h' | '12h'>('24h')
  const [itemsPerPage, setItemsPerPage] = useState('50')
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setDefaultView(doc.default_view)
    setDateFormat(doc.date_format)
    setTimeFormat(doc.time_format)
    setItemsPerPage(String(doc.items_per_page))
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
        expand_albums: doc.expand_albums,
        expand_eps: doc.expand_eps,
        expand_singles: doc.expand_singles,
        expand_broadcast: doc.expand_broadcast,
        expand_other: doc.expand_other,
        theme: doc.theme,
        color_impaired: doc.color_impaired,
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
