import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { ProfilesSettings, QualityLevel, QualityProfile, QualityProfileItem } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// ProfilesSection is the quality-profiles section: each profile picks which
// quality levels are allowed (items, referenced by QualityLevel id) and where
// a release is "good enough" (the cutoff, a QualityLevel id). Level names are
// read from the quality section document so this pane stays in lockstep with
// the ladder.
export function ProfilesSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<ProfilesSettings>('profiles')

  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [levels, setLevels] = useState<QualityLevel[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (doc) setProfiles(doc.profiles)
  }, [doc])

  // Read the quality ladder once for the level-name labels.
  useEffect(() => {
    let cancelled = false
    client
      .getSettingsSection<{ levels: QualityLevel[] }>('quality')
      .then((q) => {
        if (!cancelled) setLevels(q.levels)
      })
      .catch(() => {
        /* labels degrade to ids if the quality doc is unavailable */
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="profiles-error" />
  }

  function patchProfile(id: number, patch: Partial<QualityProfile>) {
    setProfiles((prev) => prev.map((p) => (p.id === id ? { ...p, ...patch } : p)))
  }

  function toggleLevel(profileId: number, levelId: number, approved: boolean) {
    setProfiles((prev) =>
      prev.map((p) => {
        if (p.id !== profileId) return p
        const has = p.items.some((i) => i.quality === levelId)
        const items: QualityProfileItem[] = has
          ? p.items.map((i) => (i.quality === levelId ? { ...i, approved } : i))
          : [...p.items, { quality: levelId, approved }]
        return { ...p, items }
      }),
    )
  }

  function addProfile() {
    const id = Date.now()
    const items: QualityProfileItem[] = levels.map((l) => ({ quality: l.id, approved: false }))
    setProfiles((prev) => [...prev, { id, name: `Profile ${prev.length + 1}`, cutoff: levels[0]?.id ?? 0, items }])
  }

  function removeProfile(id: number) {
    setProfiles((prev) => prev.filter((p) => p.id !== id))
  }

  function handleSave() {
    void submit(async () => {
      const next: ProfilesSettings = { profiles }
      await client.putSettingsSection<ProfilesSettings>('profiles', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="Profiles"
      hint="Quality profiles: which levels are allowed per series, and the cutoff that marks a release acceptable."
      testId="profiles-card"
    >
      {profiles.map((p) => (
        <div key={p.id} className="card-inner" data-testid={`profile-${p.id}`}>
          <div className="row">
            <input
              value={p.name}
              onChange={(e) => patchProfile(p.id, { name: e.target.value })}
              data-testid={`profile-name-${p.id}`}
            />
            <label htmlFor={`cutoff-${p.id}`}>Cutoff</label>
            <select
              id={`cutoff-${p.id}`}
              value={p.cutoff}
              onChange={(e) => patchProfile(p.id, { cutoff: Number(e.target.value) })}
              data-testid={`profile-cutoff-${p.id}`}
            >
              <option value={0}>—</option>
              {levels.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
            <button onClick={() => removeProfile(p.id)} data-testid={`profile-del-${p.id}`}>
              Remove
            </button>
          </div>
          <div className="row wrap">
            {levels.map((l) => {
              const item = p.items.find((i) => i.quality === l.id)
              return (
                <label key={l.id} className="check small">
                  <input
                    type="checkbox"
                    checked={item?.approved ?? false}
                    onChange={(e) => toggleLevel(p.id, l.id, e.target.checked)}
                    data-testid={`profile-level-${p.id}-${l.id}`}
                  />
                  {l.name}
                </label>
              )
            })}
          </div>
        </div>
      ))}
      {saved && (
        <p className="pill pill-ok" data-testid="profiles-saved">
          Saved
        </p>
      )}
      <button onClick={addProfile} data-testid="profiles-add">
        Add profile
      </button>
      <button onClick={handleSave} disabled={busy || loading || profiles.length === 0} data-testid="profiles-save">
        {busy ? 'Saving…' : 'Save profiles'}
      </button>
    </SectionCard>
  )
}
