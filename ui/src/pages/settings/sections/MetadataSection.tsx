import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { MediaKind, MetadataSettings } from '../../../api/types'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// MetadataSection is the metadata section of the P5 settings shell: the
// metadata file writer options (NFO sidecars + subtitles on import/refresh,
// in which language, for which kinds). The NFO toggle gates the
// metawrite.Writer wired into the wanted manual-import flow (P5 wave 2).
const KINDS: MediaKind[] = ['movie', 'tv', 'music', 'book']

export function MetadataSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<MetadataSettings>('metadata')
  const [generateNfo, setGenerateNfo] = useState(true)
  const [generateSubtitles, setGenerateSubtitles] = useState(false)
  const [language, setLanguage] = useState('en')
  const [kinds, setKinds] = useState<Record<string, boolean>>({ movie: true, tv: true, music: true, book: true })
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setGenerateNfo(doc.generate_nfo)
    setGenerateSubtitles(doc.generate_subtitles)
    setLanguage(doc.language)
    setKinds(doc.kinds ?? { movie: true, tv: true, music: true, book: true })
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="metadata-error" />
  }

  function handleSave() {
    if (!doc) return
    void submit(async () => {
      const next: MetadataSettings = {
        generate_nfo: generateNfo,
        generate_subtitles: generateSubtitles,
        language,
        kinds: Object.fromEntries(KINDS.map((k) => [k, kinds[k] ?? false])),
        tag_audio_files: doc.tag_audio_files,
        scrub_existing_tags: doc.scrub_existing_tags,
        consumers: doc.consumers,
      }
      await client.putSettingsSection<MetadataSettings>('metadata', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="Metadata"
      hint="What the metadata file writer produces on import and refresh. NFO sidecars are generated next to each imported file."
      testId="metadata-card"
    >
      <label className="check" htmlFor="md-nfo">
        <input
          id="md-nfo"
          type="checkbox"
          checked={generateNfo}
          onChange={(e) => setGenerateNfo(e.target.checked)}
          data-testid="md-nfo"
        />
        Generate .nfo sidecars
      </label>
      <label className="check" htmlFor="md-subs">
        <input
          id="md-subs"
          type="checkbox"
          checked={generateSubtitles}
          onChange={(e) => setGenerateSubtitles(e.target.checked)}
          data-testid="md-subs"
        />
        Fetch sidecar subtitles
      </label>
      <label htmlFor="md-lang">Language</label>
      <input id="md-lang" value={language} onChange={(e) => setLanguage(e.target.value)} data-testid="md-lang" />
      <div data-testid="md-kinds">
        {KINDS.map((k) => (
          <label className="check" key={k}>
            <input
              type="checkbox"
              checked={kinds[k] ?? false}
              onChange={(e) => setKinds((ks) => ({ ...ks, [k]: e.target.checked }))}
              data-testid={`md-kind-${k}`}
            />
            {k}
          </label>
        ))}
      </div>
      {saved && (
        <p className="pill pill-ok" data-testid="metadata-saved">
          Saved
        </p>
      )}
      <SaveError error={saveError} testId="metadata-error-save" />
      <button onClick={handleSave} disabled={busy || loading} data-testid="metadata-save">
        {busy ? 'Saving…' : 'Save metadata'}
      </button>
    </SectionCard>
  )
}
