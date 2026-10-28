import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type {
  DelayProfile,
  MetadataProfile,
  ProfilesSettings,
  QualityLevel,
  QualityProfile,
  QualityProfileItem,
  ReleaseProfile,
} from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// ProfilesSection is the profiles section: TV quality profiles (unchanged),
// plus the Radarr R5b surfaces — movie quality profiles (the 14 seeded Radarr
// v6 defaults, ids in their 100+ space, items referencing movie quality
// level ids), delay profiles (search delay per release source, hours), and
// release profiles (which release sources may be considered). Everything
// saves as one profiles document.
//
// Lidarr L3 adds the music surfaces: music quality profiles (the 4 seeded
// Lidarr defaults, ids in the 200+ space, items referencing music quality
// level ids) and metadata profiles (Standard / Album / Studio — read-mostly
// lists of the metadata files each generates).
//
// The release source keys shared by the delay and release sub-sections.
const RELEASE_TYPES: { key: 'webrip' | 'bluray' | 'remux' | 'brdisk' | 'rawhd' | 'hdtv'; label: string }[] = [
  { key: 'webrip', label: 'Webrip' },
  { key: 'bluray', label: 'Blu-ray' },
  { key: 'remux', label: 'Remux' },
  { key: 'brdisk', label: 'BR-Disk' },
  { key: 'rawhd', label: 'Raw-HD' },
  { key: 'hdtv', label: 'HDTV' },
]

export function ProfilesSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<ProfilesSettings>('profiles')

  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [movieProfiles, setMovieProfiles] = useState<QualityProfile[]>([])
  const [delayProfiles, setDelayProfiles] = useState<DelayProfile[]>([])
  const [releaseProfiles, setReleaseProfiles] = useState<ReleaseProfile[]>([])
  const [levels, setLevels] = useState<QualityLevel[]>([])
  const [movieLevels, setMovieLevels] = useState<QualityLevel[]>([])
  // Lidarr L3: music quality profiles (the 200+ id space) + the read-mostly
  // metadata profiles. Missing keys (pre-L3 documents) seed empty — the
  // blocks render their empty state rather than throwing.
  const [musicProfiles, setMusicProfiles] = useState<QualityProfile[]>([])
  const [metadataProfiles, setMetadataProfiles] = useState<MetadataProfile[]>([])
  const [musicLevels, setMusicLevels] = useState<QualityLevel[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (doc) {
      setProfiles(doc.profiles)
      // Tolerate pre-R5 documents (and P5-shaped test fixtures) that predate
      // the movie arrays: they arrive absent, not as empty arrays.
      setMovieProfiles(doc.movie_profiles ?? [])
      setDelayProfiles(doc.delay_profiles ?? [])
      setReleaseProfiles(doc.release_profiles ?? [])
      // Lidarr L3: same tolerance for the music/metadata arrays.
      setMusicProfiles(doc.music_profiles ?? [])
      setMetadataProfiles(doc.metadata_profiles ?? [])
    }
  }, [doc])

  // Read the quality ladder (TV + movie + music) once for the level-name
  // labels and the cutoff dropdown options.
  useEffect(() => {
    let cancelled = false
    client
      .getSettingsSection<{ levels: QualityLevel[]; movie_levels: QualityLevel[]; music_levels?: QualityLevel[] }>(
        'quality',
      )
      .then((q) => {
        if (!cancelled) {
          setLevels(q.levels)
          setMovieLevels(q.movie_levels)
          setMusicLevels(q.music_levels ?? [])
        }
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

  // ---- Movie quality profiles (the 100+ id space) ----

  function patchMovieProfile(id: number, patch: Partial<QualityProfile>) {
    setMovieProfiles((prev) => prev.map((p) => (p.id === id ? { ...p, ...patch } : p)))
  }

  function toggleMovieLevel(profileId: number, levelId: number, approved: boolean) {
    setMovieProfiles((prev) =>
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

  function addMovieProfile() {
    const id = Date.now()
    const items: QualityProfileItem[] = movieLevels.map((l) => ({ quality: l.id, approved: false }))
    setMovieProfiles((prev) => [
      ...prev,
      { id, name: `Profile ${prev.length + 1}`, cutoff: movieLevels[0]?.id ?? 0, items },
    ])
  }

  function removeMovieProfile(id: number) {
    setMovieProfiles((prev) => prev.filter((p) => p.id !== id))
  }

  // ---- Music quality profiles (the 200+ id space; Lidarr L3) ----

  function patchMusicProfile(id: number, patch: Partial<QualityProfile>) {
    setMusicProfiles((prev) => prev.map((p) => (p.id === id ? { ...p, ...patch } : p)))
  }

  function toggleMusicLevel(profileId: number, levelId: number, approved: boolean) {
    setMusicProfiles((prev) =>
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

  function addMusicProfile() {
    const id = Date.now()
    const items: QualityProfileItem[] = musicLevels.map((l) => ({ quality: l.id, approved: false }))
    setMusicProfiles((prev) => [
      ...prev,
      { id, name: `Profile ${prev.length + 1}`, cutoff: musicLevels[0]?.id ?? 0, items },
    ])
  }

  function removeMusicProfile(id: number) {
    setMusicProfiles((prev) => prev.filter((p) => p.id !== id))
  }

  // ---- Delay profiles (hours per release source; 0 = no delay) ----

  function patchDelayProfile(id: number, patch: Partial<DelayProfile>) {
    setDelayProfiles((prev) => prev.map((p) => (p.id === id ? { ...p, ...patch } : p)))
  }

  function addDelayProfile() {
    const id = Date.now()
    setDelayProfiles((prev) => [
      ...prev,
      {
        id,
        name: `Delay ${prev.length + 1}`,
        description: '',
        webrip: 0,
        bluray: 0,
        remux: 0,
        brdisk: 0,
        rawhd: 0,
        hdtv: 0,
      },
    ])
  }

  function removeDelayProfile(id: number) {
    setDelayProfiles((prev) => prev.filter((p) => p.id !== id))
  }

  // ---- Release profiles (include booleans per release source) ----

  function patchReleaseProfile(id: number, patch: Partial<ReleaseProfile>) {
    setReleaseProfiles((prev) => prev.map((p) => (p.id === id ? { ...p, ...patch } : p)))
  }

  function addReleaseProfile() {
    const id = Date.now()
    setReleaseProfiles((prev) => [
      ...prev,
      {
        id,
        name: `Release ${prev.length + 1}`,
        description: '',
        webrip: true,
        bluray: true,
        remux: true,
        brdisk: true,
        rawhd: true,
        hdtv: true,
      },
    ])
  }

  function removeReleaseProfile(id: number) {
    setReleaseProfiles((prev) => prev.filter((p) => p.id !== id))
  }

  function handleSave() {
    if (!doc) return
    void submit(async () => {
      const next: ProfilesSettings = {
        profiles,
        movie_profiles: movieProfiles,
        music_profiles: musicProfiles,
        metadata_profiles: metadataProfiles,
        delay_profiles: delayProfiles,
        release_profiles: releaseProfiles,
      }
      await client.putSettingsSection<ProfilesSettings>('profiles', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="Profiles"
      hint="Quality profiles (TV and movie), and the Radarr delay and release profiles."
      testId="profiles-card"
    >
      <h4>TV quality profiles</h4>
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
      <button onClick={addProfile} data-testid="profiles-add">
        Add profile
      </button>

      <h4>Movie quality profiles</h4>
      {movieProfiles.length === 0 && <p className="dim small">No movie quality profiles.</p>}
      {movieProfiles.map((p) => (
        <div key={p.id} className="card-inner" data-testid={`mvprofile-${p.id}`}>
          <div className="row">
            <input
              value={p.name}
              onChange={(e) => patchMovieProfile(p.id, { name: e.target.value })}
              data-testid={`mvprofile-name-${p.id}`}
            />
            <label htmlFor={`mv-cutoff-${p.id}`}>Cutoff</label>
            <select
              id={`mv-cutoff-${p.id}`}
              value={p.cutoff}
              onChange={(e) => patchMovieProfile(p.id, { cutoff: Number(e.target.value) })}
              data-testid={`mvprofile-cutoff-${p.id}`}
            >
              <option value={0}>—</option>
              {movieLevels.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
            <button onClick={() => removeMovieProfile(p.id)} data-testid={`mvprofile-del-${p.id}`}>
              Remove
            </button>
          </div>
          <div className="row wrap">
            {movieLevels.map((l) => {
              const item = p.items.find((i) => i.quality === l.id)
              return (
                <label key={l.id} className="check small">
                  <input
                    type="checkbox"
                    checked={item?.approved ?? false}
                    onChange={(e) => toggleMovieLevel(p.id, l.id, e.target.checked)}
                    data-testid={`mvprofile-level-${p.id}-${l.id}`}
                  />
                  {l.name}
                </label>
              )
            })}
          </div>
        </div>
      ))}
      <button onClick={addMovieProfile} data-testid="mvprofile-add">
        Add movie profile
      </button>

      <h4>Music quality profiles</h4>
      {musicProfiles.length === 0 && <p className="dim small">No music quality profiles.</p>}
      {musicProfiles.map((p) => (
        <div key={p.id} className="card-inner" data-testid={`musicprofile-${p.id}`}>
          <div className="row">
            <input
              value={p.name}
              onChange={(e) => patchMusicProfile(p.id, { name: e.target.value })}
              data-testid={`musicprofile-name-${p.id}`}
            />
            <label htmlFor={`music-cutoff-${p.id}`}>Cutoff</label>
            <select
              id={`music-cutoff-${p.id}`}
              value={p.cutoff}
              onChange={(e) => patchMusicProfile(p.id, { cutoff: Number(e.target.value) })}
              data-testid={`musicprofile-cutoff-${p.id}`}
            >
              <option value={0}>—</option>
              {musicLevels.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
            <button onClick={() => removeMusicProfile(p.id)} data-testid={`musicprofile-del-${p.id}`}>
              Remove
            </button>
          </div>
          <div className="row wrap">
            {musicLevels.map((l) => {
              const item = p.items.find((i) => i.quality === l.id)
              return (
                <label key={l.id} className="check small">
                  <input
                    type="checkbox"
                    checked={item?.approved ?? false}
                    onChange={(e) => toggleMusicLevel(p.id, l.id, e.target.checked)}
                    data-testid={`musicprofile-level-${p.id}-${l.id}`}
                  />
                  {l.name}
                </label>
              )
            })}
          </div>
        </div>
      ))}
      <button onClick={addMusicProfile} data-testid="musicprofile-add">
        Add music profile
      </button>

      <h4>Metadata profiles</h4>
      {metadataProfiles.length === 0 && <p className="dim small">No metadata profiles.</p>}
      {metadataProfiles.map((m) => (
        <div key={m.id} className="card-inner" data-testid={`metadataprofile-${m.id}`}>
          <div className="row">
            <strong>{m.name}</strong>
            {m.description && <span className="dim small">{m.description}</span>}
          </div>
          <div className="row wrap" data-testid={`metadataprofile-items-${m.id}`}>
            {m.items.map((item) => (
              <span key={item} className="pill">
                {item}
              </span>
            ))}
          </div>
        </div>
      ))}

      <h4>Delay profiles</h4>
      {delayProfiles.length === 0 && <p className="dim small">No delay profiles.</p>}
      {delayProfiles.map((d) => (
        <div key={d.id} className="card-inner" data-testid={`delay-${d.id}`}>
          <div className="row">
            <input
              value={d.name}
              onChange={(e) => patchDelayProfile(d.id, { name: e.target.value })}
              data-testid={`delay-name-${d.id}`}
            />
            <input
              value={d.description}
              placeholder="Description"
              onChange={(e) => patchDelayProfile(d.id, { description: e.target.value })}
              data-testid={`delay-desc-${d.id}`}
            />
            <button onClick={() => removeDelayProfile(d.id)} data-testid={`delay-del-${d.id}`}>
              Remove
            </button>
          </div>
          <div className="row wrap">
            {RELEASE_TYPES.map(({ key, label }) => (
              <div key={key} className="col">
                <label htmlFor={`delay-hours-${d.id}-${key}`}>{label} (hours)</label>
                <input
                  id={`delay-hours-${d.id}-${key}`}
                  type="number"
                  value={d[key]}
                  onChange={(e) => patchDelayProfile(d.id, { [key]: Number(e.target.value) || 0 })}
                  data-testid={`delay-hours-${d.id}-${key}`}
                />
              </div>
            ))}
          </div>
        </div>
      ))}
      <button onClick={addDelayProfile} data-testid="delay-add">
        Add delay profile
      </button>

      <h4>Release profiles</h4>
      {releaseProfiles.length === 0 && <p className="dim small">No release profiles.</p>}
      {releaseProfiles.map((r) => (
        <div key={r.id} className="card-inner" data-testid={`rel-${r.id}`}>
          <div className="row">
            <input
              value={r.name}
              onChange={(e) => patchReleaseProfile(r.id, { name: e.target.value })}
              data-testid={`rel-name-${r.id}`}
            />
            <input
              value={r.description}
              placeholder="Description"
              onChange={(e) => patchReleaseProfile(r.id, { description: e.target.value })}
              data-testid={`rel-desc-${r.id}`}
            />
            <button onClick={() => removeReleaseProfile(r.id)} data-testid={`rel-del-${r.id}`}>
              Remove
            </button>
          </div>
          <div className="row wrap">
            {RELEASE_TYPES.map(({ key, label }) => (
              <label key={key} className="check small">
                <input
                  type="checkbox"
                  checked={r[key]}
                  onChange={(e) => patchReleaseProfile(r.id, { [key]: e.target.checked })}
                  data-testid={`rel-flag-${r.id}-${key}`}
                />
                {label}
              </label>
            ))}
          </div>
        </div>
      ))}
      <button onClick={addReleaseProfile} data-testid="release-add">
        Add release profile
      </button>

      {saved && (
        <p className="pill pill-ok" data-testid="profiles-saved">
          Saved
        </p>
      )}
      <button onClick={handleSave} disabled={busy || loading || profiles.length === 0} data-testid="profiles-save">
        {busy ? 'Saving…' : 'Save profiles'}
      </button>
    </SectionCard>
  )
}
