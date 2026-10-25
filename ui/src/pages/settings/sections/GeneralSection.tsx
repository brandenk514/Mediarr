import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { GeneralSettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// GeneralSection is the app-level section of the P5 settings shell: host /
// port / base URL / SSL and the auth toggles. It is reconciled with the
// env-based boot config — values apply at the next boot (the hint says so).
export function GeneralSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<GeneralSettings>('general')

  const [host, setHost] = useState('')
  const [port, setPort] = useState('8080')
  const [baseUrl, setBaseUrl] = useState('')
  const [ssl, setSsl] = useState(false)
  const [authEnabled, setAuthEnabled] = useState(true)
  const [authPinning, setAuthPinning] = useState(true)
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setHost(doc.host)
    setPort(String(doc.port))
    setBaseUrl(doc.base_url)
    setSsl(doc.ssl)
    setAuthEnabled(doc.auth_enabled)
    setAuthPinning(doc.auth_pinning)
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="general-error" />
  }

  function handleSave() {
    void submit(async () => {
      const next: GeneralSettings = {
        host,
        port: Number(port) || 0,
        base_url: baseUrl,
        ssl,
        auth_enabled: authEnabled,
        auth_pinning: authPinning,
      }
      await client.putSettingsSection<GeneralSettings>('general', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="General"
      hint="App-level settings. These are reconciled with the boot config and take effect on the next start."
      testId="general-card"
    >
      <label htmlFor="gen-host">Host</label>
      <input id="gen-host" value={host} onChange={(e) => setHost(e.target.value)} data-testid="gen-host" />
      <label htmlFor="gen-port">Port</label>
      <input
        id="gen-port"
        type="number"
        value={port}
        onChange={(e) => setPort(e.target.value)}
        data-testid="gen-port"
      />
      <label htmlFor="gen-base">Base URL</label>
      <input id="gen-base" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} data-testid="gen-base" />
      <label className="check" htmlFor="gen-ssl">
        <input
          id="gen-ssl"
          type="checkbox"
          checked={ssl}
          onChange={(e) => setSsl(e.target.checked)}
          data-testid="gen-ssl"
        />
        Use SSL
      </label>
      <label className="check" htmlFor="gen-auth">
        <input
          id="gen-auth"
          type="checkbox"
          checked={authEnabled}
          onChange={(e) => setAuthEnabled(e.target.checked)}
          data-testid="gen-auth"
        />
        Auth enabled
      </label>
      <label className="check" htmlFor="gen-pin">
        <input
          id="gen-pin"
          type="checkbox"
          checked={authPinning}
          onChange={(e) => setAuthPinning(e.target.checked)}
          data-testid="gen-pin"
        />
        Auth pinning (require a token)
      </label>
      {saved && (
        <p className="pill pill-ok" data-testid="general-saved">
          Saved
        </p>
      )}
      <button onClick={handleSave} disabled={busy || loading} data-testid="general-save">
        {busy ? 'Saving…' : 'Save general'}
      </button>
    </SectionCard>
  )
}
