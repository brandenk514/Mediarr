import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { QualityLevel, QualitySettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// QualitySection is the quality-definitions section: the TV quality ladder
// (S-DR / HDTV / WEBDL / 1080p / 2160p …) and — since Radarr R5b — the Radarr
// movie quality definitions (movie_levels, a separate id space starting at
// 100). Both render as definition tables (Quality · Source · Size Limit) and
// save as one document; the TV table is unchanged.
//
// Lidarr L3 adds the music quality definitions (music_levels, the 200+ id
// space): a 38-row lossy/lossless ladder rendered read-mostly (Quality ·
// Title · Size Limit, 0 = "Unlimited") with a "Reset Definitions" button.

// FACTORY_MUSIC_LEVELS mirrors internal/settings.DefaultMusicLevels() (the
// 38-row Lidarr ladder, best-first: the lossless group, then High/Mid/Low/
// Poor/Trash Quality Lossy, Unknown last). There is no reset endpoint on the
// server — the "Reset Definitions" button PUTs the document with this table,
// so the two must stay in lockstep if the backend seed changes.
const FACTORY_MUSIC_LEVELS: QualityLevel[] = (
  [
    [213, 'WAV', '', 0],
    [235, 'APE', 'Lossless', 0],
    [236, 'WavPack', 'Lossless', 0],
    [221, 'FLAC 24bit', 'Lossless', 0],
    [237, 'ALAC 24bit', 'Lossless', 0],
    [206, 'FLAC', 'Lossless', 0],
    [207, 'ALAC', 'Lossless', 0],
    [214, 'OGG Vorbis Q10', 'High Quality Lossy', 550],
    [211, 'AAC-320', 'High Quality Lossy', 350],
    [212, 'AAC-VBR', 'High Quality Lossy', 350],
    [204, 'MP3-320', 'High Quality Lossy', 350],
    [202, 'MP3-VBR-V0', 'High Quality Lossy', 350],
    [215, 'OGG Vorbis Q9', 'High Quality Lossy', 350],
    [210, 'AAC-256', 'Mid Quality Lossy', 280],
    [203, 'MP3-256', 'Mid Quality Lossy', 280],
    [208, 'MP3-VBR-V2', 'Mid Quality Lossy', 280],
    [216, 'OGG Vorbis Q8', 'Mid Quality Lossy', 280],
    [217, 'OGG Vorbis Q7', 'Mid Quality Lossy', 245],
    [220, 'WMA', 'Low Quality Lossy', 350],
    [234, 'MP3-224', 'Low Quality Lossy', 245],
    [209, 'AAC-192', 'Low Quality Lossy', 210],
    [201, 'MP3-192', 'Low Quality Lossy', 210],
    [218, 'OGG Vorbis Q6', 'Low Quality Lossy', 210],
    [205, 'MP3-160', 'Poor Quality Lossy', 175],
    [219, 'OGG Vorbis Q5', 'Poor Quality Lossy', 175],
    [222, 'MP3-128', 'Poor Quality Lossy', 140],
    [233, 'MP3-112', 'Poor Quality Lossy', 125],
    [223, 'MP3-96', 'Poor Quality Lossy', 110],
    [224, 'MP3-80', 'Trash Quality Lossy', 95],
    [225, 'MP3-64', 'Trash Quality Lossy', 75],
    [226, 'MP3-56', 'Trash Quality Lossy', 65],
    [227, 'MP3-48', 'Trash Quality Lossy', 55],
    [228, 'MP3-40', 'Trash Quality Lossy', 45],
    [229, 'MP3-32', 'Trash Quality Lossy', 40],
    [230, 'MP3-24', 'Trash Quality Lossy', 30],
    [231, 'MP3-16', 'Trash Quality Lossy', 20],
    [232, 'MP3-8', 'Trash Quality Lossy', 10],
    [200, 'Unknown', '', 350],
  ] as [number, string, string, number][]
).map(([id, name, group, sizeLimitMb]) => ({
  id,
  name,
  group,
  source: '',
  resolution: '',
  size_limit_mb: sizeLimitMb,
}))

// sizeLimit renders the Size Limit column the Lidarr way: 0 = "Unlimited".
function sizeLimit(mb: number): string {
  return mb === 0 ? 'Unlimited' : `${mb} MB`
}

export function QualitySection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<QualitySettings>('quality')

  const [levels, setLevels] = useState<QualityLevel[]>([])
  const [movieLevels, setMovieLevels] = useState<QualityLevel[]>([])
  // Lidarr L3: the music ladder. A missing key (pre-L3 documents) seeds an
  // empty table — the block renders its empty state rather than throwing.
  const [musicLevels, setMusicLevels] = useState<QualityLevel[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (doc) {
      setLevels(doc.levels)
      setMovieLevels(doc.movie_levels ?? [])
      setMusicLevels(doc.music_levels ?? [])
    }
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="quality-error" />
  }

  function patchLevel(id: number, field: keyof QualityLevel, value: string | number) {
    setLevels((prev) => prev.map((l) => (l.id === id ? { ...l, [field]: value } : l)))
  }

  function patchMovieLevel(id: number, field: keyof QualityLevel, value: string | number) {
    setMovieLevels((prev) => prev.map((l) => (l.id === id ? { ...l, [field]: value } : l)))
  }

  function patchMusicLevel(id: number, field: keyof QualityLevel, value: string | number) {
    setMusicLevels((prev) => prev.map((l) => (l.id === id ? { ...l, [field]: value } : l)))
  }

  function handleSave() {
    if (!doc) return
    void submit(async () => {
      const next: QualitySettings = { levels, movie_levels: movieLevels, music_levels: musicLevels }
      await client.putSettingsSection<QualitySettings>('quality', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  // Reset Definitions: PUT the full document with the factory music ladder
  // (the server has no reset endpoint; the factory table is a component
  // constant mirroring the backend seed — see FACTORY_MUSIC_LEVELS).
  function handleResetMusic() {
    if (!doc) return
    setMusicLevels(FACTORY_MUSIC_LEVELS)
    void submit(async () => {
      const next: QualitySettings = {
        levels,
        movie_levels: movieLevels,
        music_levels: FACTORY_MUSIC_LEVELS,
      }
      await client.putSettingsSection<QualitySettings>('quality', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="Quality"
      hint="The quality ladder a release is judged against, lowest to highest."
      testId="quality-card"
    >
      <h4>TV quality definitions</h4>
      <table className="table" data-testid="quality-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Name</th>
            <th>Source</th>
            <th>Resolution</th>
            <th>Max size (MB)</th>
          </tr>
        </thead>
        <tbody>
          {levels.map((l, i) => (
            <tr key={l.id}>
              <td>{i + 1}</td>
              <td>
                <input
                  value={l.name}
                  onChange={(e) => patchLevel(l.id, 'name', e.target.value)}
                  data-testid={`quality-name-${l.id}`}
                />
              </td>
              <td>
                <input
                  value={l.source}
                  onChange={(e) => patchLevel(l.id, 'source', e.target.value)}
                  data-testid={`quality-source-${l.id}`}
                />
              </td>
              <td>
                <input
                  value={l.resolution}
                  onChange={(e) => patchLevel(l.id, 'resolution', e.target.value)}
                  data-testid={`quality-res-${l.id}`}
                />
              </td>
              <td>
                <input
                  type="number"
                  value={l.size_limit_mb}
                  onChange={(e) => patchLevel(l.id, 'size_limit_mb', Number(e.target.value) || 0)}
                  data-testid={`quality-size-${l.id}`}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h4>Movie quality definitions</h4>
      <table className="table" data-testid="movie-quality-table">
        <thead>
          <tr>
            <th>Quality</th>
            <th>Source</th>
            <th>Resolution</th>
            <th>Size limit (MB)</th>
          </tr>
        </thead>
        <tbody>
          {movieLevels.map((l) => (
            <tr key={l.id} data-testid={`movie-quality-row-${l.id}`}>
              <td>
                <input
                  value={l.name}
                  onChange={(e) => patchMovieLevel(l.id, 'name', e.target.value)}
                  data-testid={`movie-quality-name-${l.id}`}
                />
              </td>
              <td>
                <input
                  value={l.source}
                  onChange={(e) => patchMovieLevel(l.id, 'source', e.target.value)}
                  data-testid={`movie-quality-source-${l.id}`}
                />
              </td>
              <td>
                <input
                  value={l.resolution}
                  onChange={(e) => patchMovieLevel(l.id, 'resolution', e.target.value)}
                  data-testid={`movie-quality-res-${l.id}`}
                />
              </td>
              <td>
                <input
                  type="number"
                  value={l.size_limit_mb}
                  onChange={(e) => patchMovieLevel(l.id, 'size_limit_mb', Number(e.target.value) || 0)}
                  data-testid={`movie-quality-size-${l.id}`}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h4>Music quality definitions</h4>
      <p className="dim small">Limits are automatically adjusted for the album duration.</p>
      {musicLevels.length === 0 && <p className="dim small">No music quality definitions.</p>}
      {musicLevels.length > 0 && (
        <table className="table" data-testid="music-quality-table">
          <thead>
            <tr>
              <th>Quality</th>
              <th>Title</th>
              <th>Size Limit</th>
            </tr>
          </thead>
          <tbody>
            {musicLevels.map((l) => (
              <tr key={l.id} data-testid={`music-quality-row-${l.id}`}>
                <td>
                  <input
                    value={l.name}
                    onChange={(e) => patchMusicLevel(l.id, 'name', e.target.value)}
                    data-testid={`music-quality-name-${l.id}`}
                  />
                </td>
                <td data-testid={`music-quality-group-${l.id}`}>{l.group}</td>
                <td data-testid={`music-quality-size-${l.id}`}>{sizeLimit(l.size_limit_mb)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <button onClick={handleResetMusic} disabled={busy || loading} data-testid="music-quality-reset">
        Reset Definitions
      </button>
      {saved && (
        <p className="pill pill-ok" data-testid="quality-saved">
          Saved
        </p>
      )}
      <button onClick={handleSave} disabled={busy || loading || levels.length === 0} data-testid="quality-save">
        {busy ? 'Saving…' : 'Save quality'}
      </button>
    </SectionCard>
  )
}
