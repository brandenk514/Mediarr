import { useCallback, useEffect, useState } from 'react'
import { ApiError, client } from '../../api/client'
import type { UnmappedFile } from '../../api/types'
import { humanBytes, relTime } from '../../components/music'

// UnmappedFilesPage is the Lidarr L5 "Unmapped Files" page (music's import
// path): a read-only listing of the audio files under the media root that are
// NOT mapped to a monitored artist, in the Lidarr column set
// (Path · Size · Date Added · Quality). A file under a folder named after a
// monitored artist is considered mapped and excluded; everything else (other
// folders, or files loose directly under the root) is unmapped. The walk
// performs no moves or deletions.
const DASH = '—'

export function UnmappedFilesPage() {
  const [files, setFiles] = useState<UnmappedFile[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)

  const load = useCallback(async () => {
    try {
      const f = await client.listUnmappedFiles()
      setFiles(f ?? []) // older backends may return null for an empty walk
      setError(null)
      setAuthLost(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      if (e instanceof ApiError && e.status === 404) {
        setError('No music root folder is configured.')
        return
      }
      setError(e instanceof Error ? e.message : 'failed to load unmapped files')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (authLost) {
    return (
      <div className="card" data-testid="music-unmapped">
        <h2>Unmapped Files</h2>
        <p className="error">Your session expired — please sign in again.</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="card" data-testid="music-unmapped">
        <h2>Unmapped Files</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (!files) {
    return (
      <div className="card" data-testid="music-unmapped">
        <h2>Unmapped Files</h2>
        <p className="dim">Loading unmapped files…</p>
      </div>
    )
  }

  return (
    <div data-testid="music-unmapped">
      <div className="card">
        <h2 style={{ margin: '0 0 0.5rem' }}>Unmapped Files</h2>
        <p className="dim" style={{ marginTop: 0 }}>
          Audio files in your music library that are not mapped to a monitored artist.{' '}
          {files.length === 0
            ? 'Nothing is unmapped.'
            : `${files.length} file${files.length === 1 ? '' : 's'} · ${humanBytes(
                files.reduce((sum, f) => sum + f.size, 0),
              )} total`}
        </p>
        {files.length === 0 ? (
          <p className="dim" data-testid="music-unmapped-empty">
            Every audio file is mapped to a monitored artist.
          </p>
        ) : (
          <div className="list-table">
            <div className="lt-head">
              <span>Path</span>
              <span>Size</span>
              <span>Date Added</span>
              <span>Quality</span>
            </div>
            {files.map((f, i) => (
              <div className="lt-row" data-testid={`music-unmapped-row-${i}`} key={f.path}>
                <span title={f.path}>{f.path}</span>
                <span className="lt-dim">{humanBytes(f.size)}</span>
                <span className="lt-dim">{relTime(f.date_added)}</span>
                <span>{f.quality || DASH}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
