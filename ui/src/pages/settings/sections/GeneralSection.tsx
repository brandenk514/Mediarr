import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { GeneralSettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// Prowlarr PR4: the log verbosity levels the server accepts.
const LOG_LEVELS: GeneralSettings['log_level'][] = ['debug', 'info', 'warn', 'error']

// GeneralSection is the app-level section of the P5 settings shell: host /
// port / base URL / SSL and the auth toggles, plus the Prowlarr PR4 security
// + operations block (allowed hosts, certificate validation, outbound HTTP
// proxy, log level, analytics opt-out). It is reconciled with the
// env-based boot config — values apply at the next boot (the hint says so).
export function GeneralSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<GeneralSettings>('general')

  const [host, setHost] = useState('')
  const [port, setPort] = useState('8080')
  const [baseUrl, setBaseUrl] = useState('')
  const [ssl, setSsl] = useState(false)
  const [authEnabled, setAuthEnabled] = useState(true)
  const [authPinning, setAuthPinning] = useState(true)
  // Prowlarr PR4 fields. The ?? defaults mirror the server's Document
  // fallbacks for documents that predate the fields (a stale saved doc
  // arrives with the keys absent — a missing key must seed the default,
  // or the shell's stale-fixture render would throw).
  const [allowedHosts, setAllowedHosts] = useState('')
  const [certValidation, setCertValidation] = useState(true)
  const [httpProxy, setHttpProxy] = useState('')
  const [logLevel, setLogLevel] = useState<GeneralSettings['log_level']>('info')
  const [analytics, setAnalytics] = useState(false)
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
    setAllowedHosts(doc.allowed_hosts ?? '')
    setCertValidation(doc.cert_validation ?? true)
    setHttpProxy(doc.http_proxy ?? '')
    setLogLevel(doc.log_level ?? 'info')
    setAnalytics(doc.analytics ?? false)
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
        allowed_hosts: allowedHosts,
        cert_validation: certValidation,
        http_proxy: httpProxy,
        log_level: logLevel,
        analytics,
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

      {/* Prowlarr PR4: security + operations. The restart-hinted fields apply
          on the next start, like the rest of the boot-reconciled document. */}
      <h4>Security</h4>
      <label htmlFor="gen-allowed-hosts">Allowed hosts</label>
      <p className="dim small">Comma-separated hostnames the server may be reached on; empty = no restriction.</p>
      <input
        id="gen-allowed-hosts"
        value={allowedHosts}
        onChange={(e) => setAllowedHosts(e.target.value)}
        placeholder="media.local, 192.168.1.50"
        data-testid="gen-allowed-hosts"
      />
      <label className="check" htmlFor="gen-cert-validation">
        <input
          id="gen-cert-validation"
          type="checkbox"
          checked={certValidation}
          onChange={(e) => setCertValidation(e.target.checked)}
          data-testid="gen-cert-validation"
        />
        Validate TLS certificates on outbound connections
      </label>

      <h4>Network &amp; logging</h4>
      <label htmlFor="gen-http-proxy">Outbound HTTP proxy (requires restart)</label>
      <input
        id="gen-http-proxy"
        value={httpProxy}
        onChange={(e) => setHttpProxy(e.target.value)}
        placeholder="http://proxy:3128"
        data-testid="gen-http-proxy"
      />
      <label htmlFor="gen-log-level">Log level (requires restart)</label>
      <select
        id="gen-log-level"
        value={logLevel}
        onChange={(e) => setLogLevel(e.target.value as GeneralSettings['log_level'])}
        data-testid="gen-log-level"
      >
        {LOG_LEVELS.map((l) => (
          <option key={l} value={l}>
            {l}
          </option>
        ))}
      </select>
      <label className="check" htmlFor="gen-analytics">
        <input
          id="gen-analytics"
          type="checkbox"
          checked={analytics}
          onChange={(e) => setAnalytics(e.target.checked)}
          data-testid="gen-analytics"
        />
        Send anonymous usage analytics (opt-in; off by default)
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
