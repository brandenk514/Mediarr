import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { QualityLevel, QualitySettings } from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// QualitySection is the quality-definitions section: the TV quality ladder
// (S-DR / HDTV / WEBDL / 1080p / 2160p …) and — since Radarr R5b — the Radarr
// movie quality definitions (movie_levels, a separate id space starting at
// 100). Both render as definition tables (Quality · Source · Size Limit) and
// save as one document; the TV table is unchanged.
export function QualitySection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<QualitySettings>('quality')

  const [levels, setLevels] = useState<QualityLevel[]>([])
  const [movieLevels, setMovieLevels] = useState<QualityLevel[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (doc) {
      setLevels(doc.levels)
      setMovieLevels(doc.movie_levels ?? [])
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

  function handleSave() {
    void submit(async () => {
      const next: QualitySettings = { levels, movie_levels: movieLevels }
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
