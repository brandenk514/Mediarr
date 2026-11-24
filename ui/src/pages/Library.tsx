// Library.tsx — the unified cross-kind Library grid (issue #119).
//
// A single /library route is the Library section's home: it replaces the four
// per-kind list pages as the destination the sidebar routes to, without
// changing or removing them (they stay fully routed for direct URLs and the
// deep sub-links). "All" fans out to all four list endpoints (page 1, page
// size 25) in parallel and renders the results as one grid grouped into kind
// sections in the deterministic kind order (Movies, TV, Music, Books), each
// headed with the kind label + that kind's total count. A single-kind view
// hits only that kind's endpoint with full per-kind pagination.
//
// Filters (the toolbar holds exactly these three):
//   - Kind (All / TV / Movies / Music / Books) — URL-synced both directions
//     (?kind=…), so views are deep-linkable. 'all' carries no param.
//   - Genre — client-side over the loaded items. Options are the sorted union
//     of the loaded items' genres (Movies' Genres[] and Music's genre); TV and
//     Books carry no genre, so selecting one hides them (accepted).
//   - Status (All / Monitored / Unmonitored) — server-side via the ListQuery
//     `monitored` param, on every kind.
//
// A per-kind fetch failure degrades to a dim per-section error note while the
// other kinds still render (the R3/#118 keyless pattern); only a total
// failure (every leg rejects) is a page-level error with Retry.
//
// The data and API stay exactly as-is (no backend change): the four per-kind
// list endpoints are fanned out client-side, mirroring Discover / GlobalSearch.

import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { client } from '../api/client'
import type {
  ListQuery,
  PageArtist,
  PageAuthor,
  PageMovie,
  PageSeries,
} from '../api/types'
import { PosterCard } from '../components/primitives'
import { DEFAULT_PAGE_SIZE, Pagination } from '../components/list'
import { Menu, Toolbar, type MenuOption } from '../components/ui'

// ---- pure model (unit-tested) ---------------------------------------------

export type KindId = 'movie' | 'tv' | 'music' | 'book'

// Deterministic kind order for the "All" view's grouped sections.
export const LIBRARY_KINDS: KindId[] = ['movie', 'tv', 'music', 'book']

// Human kind labels (the GlobalSearch/Discover convention).
export const LIBRARY_KIND_LABEL: Record<KindId, string> = {
  movie: 'Movies',
  tv: 'TV',
  music: 'Music',
  book: 'Books',
}

export type KindFilter = 'all' | KindId
export type StatusFilter = 'all' | 'monitored' | 'unmonitored'

// A normalized library cell: the four wire list shapes differ (genres, rating,
// cover), so the grid renders this uniform shape.
export interface LibraryItem {
  id: number
  kind: KindId
  title: string
  /** Poster / cover URL when the wire shape carries one (Music's Cover Art
   *  Archive front cover); null = the initials stand-in (TV / Movies / Books
   *  carry no art in the list contract). */
  image: string | null
  monitored: boolean
  year: number | null
  quality: string
  rating: number | null
  genres: string[]
}

export interface KindData {
  items: LibraryItem[]
  total: number
  /** Set when this kind's list endpoint rejected; the section renders a dim
   *  note instead of a grid while the other kinds still render. */
  error: string | null
}

// The detail route a grid cell navigates to, per kind. Books land on the
// /books/:id author detail page (the /books/:id route IS the author detail).
export function detailPathFor(kind: KindId, id: number): string {
  switch (kind) {
    case 'movie':
      return `/movies/${id}`
    case 'tv':
      return `/tv/${id}`
    case 'music':
      return `/music/${id}`
    case 'book':
      return `/books/${id}`
  }
}

// The card's meta line, composed per kind with no stray separators (empty
// parts are dropped before joining).
//   TV: year + quality profile · Movies: year · Music: rating or genre if
//   present · Books: title only (no meta line).
export function itemMeta(item: LibraryItem): string {
  const parts: string[] = []
  switch (item.kind) {
    case 'tv':
      if (item.year) parts.push(String(item.year))
      if (item.quality) parts.push(item.quality)
      break
    case 'movie':
      if (item.year) parts.push(String(item.year))
      break
    case 'music':
      if (item.rating != null) parts.push(String(item.rating))
      else if (item.genres.length) parts.push(item.genres[0])
      break
    case 'book':
      break // title only
  }
  return parts.join(' · ')
}

// The sorted union of genres across the given items (Movies' Genres[] and
// Music's single genre); TV/Books contribute none.
export function genreOptions(items: LibraryItem[]): string[] {
  const set = new Set<string>()
  for (const it of items) {
    for (const g of it.genres) {
      if (g) set.add(g)
    }
  }
  return [...set].sort()
}

// Client-side genre narrowing: 'all' keeps everything; otherwise only items
// carrying the genre survive (TV/Books never do — they have no genres).
export function filterByGenre(items: LibraryItem[], genre: string): LibraryItem[] {
  if (genre === 'all') return items
  return items.filter((it) => it.genres.includes(genre))
}

// Normalize a wire list page into library cells.
export function toLibraryItems(
  kind: KindId,
  page: PageMovie | PageSeries | PageArtist | PageAuthor,
): LibraryItem[] {
  switch (kind) {
    case 'movie':
      return (page as PageMovie).items.map((m) => ({
        id: m.ID,
        kind,
        title: m.Title,
        image: null,
        monitored: m.Monitored,
        year: m.Year || null,
        quality: m.QualityProfile || '',
        rating: null,
        genres: m.Genres ?? [],
      }))
    case 'tv':
      return (page as PageSeries).items.map((s) => ({
        id: s.ID,
        kind,
        title: s.Title,
        image: null,
        monitored: s.Monitored,
        year: s.Year || null,
        quality: s.QualityProfile || '',
        rating: null,
        genres: [],
      }))
    case 'music':
      return (page as PageArtist).items.map((a) => ({
        id: a.ID,
        kind,
        title: a.Name,
        image: a.Cover || null,
        monitored: a.Monitored,
        year: null,
        quality: a.QualityProfile || '',
        rating: a.Rating != null ? a.Rating : null,
        genres: a.Genre ? [a.Genre] : [],
      }))
    case 'book':
      return (page as PageAuthor).items.map((a) => ({
        id: a.ID,
        kind,
        title: a.Name,
        image: null,
        monitored: a.Monitored,
        year: null,
        quality: a.QualityProfile || '',
        rating: null,
        genres: [],
      }))
  }
  return []
}

// A human note for a rejected leg (the R3/#118 keyless wording): ApiError
// already carries a readable message; anything else is a bare failure.
export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message
  return 'request failed'
}

// ---- component ------------------------------------------------------------

const KIND_OPTIONS: MenuOption[] = [
  { value: 'all', label: 'All' },
  { value: 'tv', label: 'TV' },
  { value: 'movie', label: 'Movies' },
  { value: 'music', label: 'Music' },
  { value: 'book', label: 'Books' },
]

const STATUS_OPTIONS: MenuOption[] = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

export function LibraryPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // Kind is URL-synced (?kind=…): read it on load, write it on change. 'all'
  // (the default) carries no param, and an unknown value falls back to 'all'.
  const rawKind = searchParams.get('kind')
  const kind: KindFilter =
    rawKind && (['tv', 'movie', 'music', 'book'] as const).includes(rawKind as KindId)
      ? (rawKind as KindId)
      : 'all'

  const [genre, setGenre] = useState<string>('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [page, setPage] = useState(1)
  const [perKind, setPerKind] = useState<Record<KindId, KindData> | null>(null)
  const [loading, setLoading] = useState(true)
  const [totalError, setTotalError] = useState<string | null>(null)
  const [retryBump, setRetryBump] = useState(0)

  // Fetch: fan out to all four legs in the "All" view; hit the single kind's
  // endpoint (with paging) otherwise. A leg failure is per-kind; only an
  // all-legs failure (or the single leg failing) is page-level.
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setPerKind(null)
    setTotalError(null)

    const q: ListQuery = { page: kind === 'all' ? 1 : page, page_size: DEFAULT_PAGE_SIZE }
    if (status === 'monitored') q.monitored = true
    else if (status === 'unmonitored') q.monitored = false

    const fetchOne = async (k: KindId, pageNo: number): Promise<KindData> => {
      const kq: ListQuery = { page: pageNo, page_size: DEFAULT_PAGE_SIZE }
      if (status === 'monitored') kq.monitored = true
      else if (status === 'unmonitored') kq.monitored = false
      try {
        switch (k) {
          case 'tv': {
            const p = await client.listSeries(kq)
            return { items: toLibraryItems('tv', p), total: p.total, error: null }
          }
          case 'movie': {
            const p = await client.listMovies(kq)
            return { items: toLibraryItems('movie', p), total: p.total, error: null }
          }
          case 'music': {
            const p = await client.listArtists(kq)
            return { items: toLibraryItems('music', p), total: p.total, error: null }
          }
          case 'book': {
            const p = await client.listAuthors(kq)
            return { items: toLibraryItems('book', p), total: p.total, error: null }
          }
        }
      } catch (e) {
        return { items: [], total: 0, error: errorMessage(e) }
      }
    }

    const emptyRecord = (): Record<KindId, KindData> => ({
      tv: { items: [], total: 0, error: null },
      movie: { items: [], total: 0, error: null },
      music: { items: [], total: 0, error: null },
      book: { items: [], total: 0, error: null },
    })

    if (kind === 'all') {
      Promise.all(LIBRARY_KINDS.map((k) => fetchOne(k, 1))).then((results) => {
        if (cancelled) return
        const rec = emptyRecord()
        LIBRARY_KINDS.forEach((k, i) => {
          rec[k] = results[i]
        })
        setLoading(false)
        if (LIBRARY_KINDS.every((k) => rec[k].error != null)) {
          setTotalError('All four library feeds failed. Retry when the providers recover.')
        } else {
          setPerKind(rec)
        }
      })
    } else {
      fetchOne(kind, page).then((kd) => {
        if (cancelled) return
        setLoading(false)
        if (kd.error) {
          // A single-kind view has one leg; its failure IS the total failure.
          setTotalError(`${LIBRARY_KIND_LABEL[kind]} failed to load. Retry when the feed recovers.`)
        } else {
          const rec = emptyRecord()
          rec[kind] = kd
          setPerKind(rec)
        }
      })
    }

    return () => {
      cancelled = true
    }
  }, [kind, status, page, retryBump])

  // Switching kinds resets the (client-side) page + genre so a stale filter
  // can't hide the new kind's results, and writes the ?kind= param.
  function changeKind(k: KindFilter) {
    if (k === kind) return
    setPage(1)
    setGenre('all')
    const next = new URLSearchParams(searchParams)
    if (k === 'all') next.delete('kind')
    else next.set('kind', k)
    setSearchParams(next)
  }

  const inScopeKinds: KindId[] = kind === 'all' ? LIBRARY_KINDS : [kind]

  // Genre options = the sorted union of the loaded (in-scope) items' genres.
  const loadedItems = useMemo(() => {
    if (!perKind) return []
    const kinds = kind === 'all' ? LIBRARY_KINDS : [kind]
    return kinds.flatMap((k) => perKind[k].items)
  }, [perKind, kind])
  const genres = useMemo(() => genreOptions(loadedItems), [loadedItems])
  const activeGenre = genre !== 'all' && genres.includes(genre) ? genre : 'all'

  const genreMenuOptions: MenuOption[] = [
    { value: 'all', label: 'All genres' },
    ...genres.map((g) => ({ value: g, label: g })),
  ]

  return (
    <div className="list-page" data-testid="library-page">
      <div className="page-head">
        <h2 data-testid="library-title">Library</h2>
      </div>
      <Toolbar
        left={
          <>
            <Menu
              label="Kind"
              icon="⛁"
              options={KIND_OPTIONS}
              selected={kind}
              onSelect={(v) => changeKind(v as KindFilter)}
              testId="library-kind"
            />
            <Menu
              label="Genre"
              icon="⬡"
              options={genreMenuOptions}
              selected={activeGenre}
              onSelect={setGenre}
              testId="library-genre"
            />
            <Menu
              label="Status"
              icon="⚲"
              options={STATUS_OPTIONS}
              selected={status}
              onSelect={(v) => setStatus(v as StatusFilter)}
              testId="library-status"
            />
          </>
        }
      />
      <div className="library-body" data-testid="library-body">
        {totalError ? (
          <div className="card" data-testid="library-error">
            <p className="error" data-testid="library-error-message">
              {totalError}
            </p>
            <button
              type="button"
              data-testid="library-retry"
              onClick={() => setRetryBump((b) => b + 1)}
            >
              Retry
            </button>
          </div>
        ) : loading && !perKind ? (
          <p className="dim" data-testid="library-loading">
            Loading your library…
          </p>
        ) : (
          <>
            {inScopeKinds.map((k) => {
              const data = perKind?.[k]
              if (!data) return null
              const filtered = filterByGenre(data.items, activeGenre)
              // A failed section always shows its note; under a genre filter an
              // empty section is hidden (TV/Books vanish — they carry no genre).
              const show = data.error != null || activeGenre === 'all' || filtered.length > 0
              if (!show) return null
              return (
                <section key={k} className="library-section" data-testid={`library-section-${k}`}>
                  <h3 className="library-section-title" data-testid={`library-section-title-${k}`}>
                    {LIBRARY_KIND_LABEL[k]}
                    {data.total > 0 && <span className="dim"> ({data.total})</span>}
                  </h3>
                  {data.error != null ? (
                    <p
                      className="error small"
                      role="alert"
                      data-testid={`library-error-${k}`}
                    >
                      {LIBRARY_KIND_LABEL[k]} failed to load: {data.error}
                    </p>
                  ) : filtered.length === 0 ? (
                    <p className="dim small" data-testid={`library-empty-${k}`}>
                      No {LIBRARY_KIND_LABEL[k]} yet.
                    </p>
                  ) : (
                    <div className="poster-grid" data-testid={`library-grid-${k}`}>
                      {filtered.map((it) => (
                        <PosterCard
                          key={it.id}
                          title={it.title}
                          image={it.image ?? undefined}
                          meta={itemMeta(it) || undefined}
                          status={{ label: LIBRARY_KIND_LABEL[it.kind], tone: 'accent' }}
                          onOpen={() => navigate(detailPathFor(it.kind, it.id))}
                          testId={`library-card-${it.kind}-${it.id}`}
                        />
                      ))}
                    </div>
                  )}
                </section>
              )
            })}
            {kind !== 'all' &&
              perKind &&
              perKind[kind].error == null && (
                <Pagination
                  page={page}
                  pageSize={DEFAULT_PAGE_SIZE}
                  total={perKind[kind].total}
                  onPage={setPage}
                />
              )}
          </>
        )}
      </div>
    </div>
  )
}
