import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { client } from '../../api/client'
import type { Movie, MovieCollection } from '../../api/types'
import { firstLetter } from '../../components/poster'
import { Badge, EmptyState } from '../../components/ui'

// MovieCollectionsPage is the Radarr R3 Collections page: a grid of
// collection cards. Each card shows the collection name ('Unknown' when the
// library only knows a name-less group), the 'N missing movie(s) from
// library' line (best-effort TMDB part count; 0 when the id is unknown or
// TMDB is unavailable), the aggregated quality profile + root path
// ('Mixed' when members differ, em-dash when unknown), the aggregated genre
// chips, and the member list: poster thumb, 'Title (year)' link, the
// monitored-edition chips + monitored-state badge, and the quality profile.
// Keyless-safe: the endpoint always answers, so the page works with ''
// collection fields.
export function MovieCollectionsPage() {
  const [collections, setCollections] = useState<MovieCollection[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setError(null)
    client
      .listMovieCollections()
      .then(setCollections)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'failed to load collections'))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="collections-title">Collections</h2>
      </div>

      {error && !collections ? (
        <div className="card" data-testid="collections-error">
          <h3>Collections unavailable</h3>
          <p className="error" data-testid="collections-error-message">
            {error}
          </p>
          <button onClick={load}>Retry</button>
        </div>
      ) : !collections || collections.length === 0 ? (
        <EmptyState
          testId="collections-empty"
          title="No collections"
          message="Library movies that belong to a TMDB collection show up here."
        >
          <Link to="/movies/add" data-testid="collections-empty-add">
            + Add movie
          </Link>
        </EmptyState>
      ) : (
        <div className="collections-grid" data-testid="collections-grid">
          {collections.map((c) => (
            <CollectionCard key={c.id} c={c} />
          ))}
        </div>
      )}
    </div>
  )
}

function CollectionCard({ c }: { c: MovieCollection }) {
  const { quality, root, genres } = aggregateMembers(c.movies)
  const name = c.name || 'Unknown'
  return (
    <section className="card collection-card" data-testid={`collection-card-${c.id}`}>
      <h3 data-testid={`collection-name-${c.id}`}>{name}</h3>
      <p className="dim small" data-testid={`collection-missing-${c.id}`}>
        {c.missing_count} missing movie{c.missing_count === 1 ? '' : 's'} from library
      </p>
      <p className="dim small" data-testid={`collection-quality-${c.id}`}>
        {quality} <span aria-hidden>·</span> {root}
      </p>
      {genres.length > 0 && (
        <span className="tag-chips" data-testid={`collection-genres-${c.id}`}>
          {genres.map((g) => (
            <Badge key={g}>{g}</Badge>
          ))}
        </span>
      )}
      <ul className="collection-members" data-testid={`collection-members-${c.id}`}>
        {c.movies.map((m) => (
          <li key={m.ID} className="collection-member" data-testid={`collection-member-${m.ID}`}>
            <Link
              to={`/movies/${m.ID}`}
              className="collection-member-link"
              data-testid={`collection-member-link-${m.ID}`}
            >
              <span className="collection-poster" aria-hidden>
                {firstLetter(m.Title)}
              </span>
              <span className="collection-member-title">
                {m.Title}
                {m.Year > 0 ? ` (${m.Year})` : ''}
              </span>
            </Link>
            <span className="collection-editions" data-testid={`collection-member-editions-${m.ID}`}>
              <Badge tone={m.Monitored ? 'ok' : 'default'}>
                {m.Monitored ? 'Monitored' : 'Unmonitored'}
              </Badge>
              {(m.Editions ?? []).map((e) => (
                <Badge key={e.ID} tone={e.Monitored ? 'teal' : 'default'}>
                  {e.Label}
                </Badge>
              ))}
            </span>
            <span className="lt-dim" data-testid={`collection-member-quality-${m.ID}`}>
              {m.QualityProfile}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

// aggregateMembers folds a collection's members into the card header
// aggregates: a single quality/root when all members agree, 'Mixed' when
// they differ, and the union of member genres (sorted).
function aggregateMembers(movies: Movie[]): { quality: string; root: string; genres: string[] } {
  const aggregate = (xs: string[]): string => {
    const uniq = [...new Set(xs.filter(Boolean))]
    if (uniq.length === 0) return '—'
    return uniq.length === 1 ? uniq[0]! : 'Mixed'
  }
  const genreSet = new Set<string>()
  for (const m of movies) {
    for (const g of m.Genres ?? []) genreSet.add(g)
  }
  return {
    quality: aggregate(movies.map((m) => m.QualityProfile ?? '')),
    root: aggregate(movies.map((m) => m.RootFolder ?? '')),
    genres: [...genreSet].sort(),
  }
}
