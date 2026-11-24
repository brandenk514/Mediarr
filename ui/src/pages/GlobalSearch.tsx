// Global search results page (issue #117).
//
// The top-bar global search (pages/Layout.tsx) navigates here with the query
// in the URL (?q=…), so results are deep-linkable. This page fans out
// client-side to the four per-kind search endpoints and merges the hits into
// ONE unified grid; every result cell carries a kind Badge (TV / Movies /
// Music / Books) from the #115 design-system primitives. Per-kind isolation:
// each endpoint resolves into its own loading / error / done state, so one
// kind's failed search (e.g. a 503 from a keyless dev stack) renders an
// inline error note while the other kinds' results still render.
//
// The per-kind data and API stay exactly as-is (no backend change):
//   TV      GET /search/tv?query=          (client.searchTv)
//   Movies  GET /search/movies?query=      (client.searchMovies)
//   Music   GET /search/artists?query=     (client.searchArtists)
//   Books   GET /search/books/lookup?query= (client.lookupBooks)

import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { client } from '../api/client'
import { Badge, PosterCard } from '../components/primitives'
import { EmptyState } from '../components/ui'
import {
  fanoutMessage,
  freshFanoutStates,
  GLOBAL_SEARCH_KIND_LABEL,
  GLOBAL_SEARCH_KINDS,
  mapBookHits,
  mapMovieHits,
  mapMusicHits,
  mapTvHits,
  type GlobalSearchHit,
  type GlobalSearchKind,
  type KindFanoutState,
} from './globalSearchModel'

// ---- page ------------------------------------------------------------------

/**
 * GlobalSearchPage is the first-class global search results route
 * (PATHS.globalSearch = /search/all?q=…). With a query it fans the four
 * per-kind searches out in parallel; each kind renders its own status chip,
 * its hits into the shared grid (badged with its kind), and an inline error
 * note when its endpoint fails.
 */
export function GlobalSearchPage() {
  const [params] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const [states, setStates] = useState<Record<GlobalSearchKind, KindFanoutState>>(freshFanoutStates)

  useEffect(() => {
    if (!q) {
      setStates(freshFanoutStates())
      return
    }
    let cancelled = false
    const loading = freshFanoutStates()
    for (const k of GLOBAL_SEARCH_KINDS) loading[k] = { phase: 'loading', hits: [] }
    setStates(loading)

    const settle = (kind: GlobalSearchKind) => (
      run: () => Promise<KindFanoutState>
    ) => {
      run()
        .then((st) => {
          if (!cancelled) setStates((prev) => ({ ...prev, [kind]: st }))
        })
        .catch((e) => {
          if (!cancelled)
            setStates((prev) => ({ ...prev, [kind]: { phase: 'error', hits: [], error: fanoutMessage(e) } }))
        })
    }

    // Four independent fan-out legs. Each is its own promise chain with its
    // own error path: one kind rejecting never touches the others.
    settle('tv')(() => client.searchTv(q).then((rows) => ({ phase: 'done', hits: mapTvHits(rows) })))
    settle('movie')(() =>
      client.searchMovies(q).then((rows) => ({ phase: 'done', hits: mapMovieHits(rows) })),
    )
    settle('music')(() =>
      client.searchArtists(q).then((res) => ({ phase: 'done', hits: mapMusicHits(res) })),
    )
    settle('book')(() =>
      client.lookupBooks(q).then((rows) => ({ phase: 'done', hits: mapBookHits(rows) })),
    )

    return () => {
      cancelled = true
    }
  }, [q])

  const allHits = GLOBAL_SEARCH_KINDS.flatMap((k) => states[k].hits)
  const anyLoading = GLOBAL_SEARCH_KINDS.some((k) => states[k].phase === 'loading')
  const errored = GLOBAL_SEARCH_KINDS.filter((k) => states[k].phase === 'error')
  // Per-kind cell index (the grid's testids are per-kind, so the first Movie
  // cell is global-search-card-movie-0 regardless of how many TV cells lead
  // it in the unified grid).
  const perKindIndex: Record<GlobalSearchKind, number> = { tv: 0, movie: 0, music: 0, book: 0 }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="global-search-title">Global search</h2>
      </div>

      {!q ? (
        <EmptyState
          title="Search all libraries"
          message="Type a query in the search bar at the top of the page — results land here (TV, Movies, Music, Books)."
          testId="global-search-idle"
        />
      ) : (
        <>
          <p className="dim small" data-testid="global-search-query">
            Results for “{q}”
          </p>

          {/* Per-kind fan-out status (loading / count / failed). */}
          <div className="flex flex-wrap items-center gap-1.5" data-testid="global-search-status">
            {GLOBAL_SEARCH_KINDS.map((k) => (
              <KindStatusChip key={k} kind={k} state={states[k]} />
            ))}
          </div>

          {/* One unified grid across all four kinds. */}
          {allHits.length > 0 ? (
            <div className="poster-grid" data-testid="global-search-grid">
              {allHits.map((h) => {
                const i = perKindIndex[h.kind]
                perKindIndex[h.kind] += 1
                return <GlobalSearchCell key={`${h.kind}-${i}`} hit={h} index={i} />
              })}
            </div>
          ) : anyLoading ? (
            <div className="card" data-testid="global-search-loading">
              <p className="dim">Searching TV, movies, music, books…</p>
            </div>
          ) : (
            <EmptyState
              title="No results"
              message={`Nothing matched “${q}” in TV, movies, music, or books.`}
              testId="global-search-empty"
            />
          )}

          {/* Per-kind isolation: a failed kind shows an inline note; the
              other kinds' results still render above. */}
          {errored.map((k) => (
            <p key={k} className="error small" role="alert" data-testid={`global-search-error-${k}`}>
              {GLOBAL_SEARCH_KIND_LABEL[k]} search failed: {states[k].error}
            </p>
          ))}
        </>
      )}
    </div>
  )
}

// KindStatusChip is one kind's fan-out status in the status row:
// label · count (done) / label · searching… (loading) / label · failed (error).
function KindStatusChip({ kind, state }: { kind: GlobalSearchKind; state: KindFanoutState }) {
  const label = GLOBAL_SEARCH_KIND_LABEL[kind]
  const text =
    state.phase === 'loading'
      ? `${label} · searching…`
      : state.phase === 'error'
        ? `${label} · failed`
        : state.phase === 'done'
          ? `${label} · ${state.hits.length}`
          : label
  const tone = state.phase === 'error' ? 'error' : state.phase === 'loading' ? 'info' : 'accent'
  return (
    <Badge tone={tone} testId={`global-search-status-${kind}`}>
      {text}
    </Badge>
  )
}

// GlobalSearchCell is one result in the unified grid: a PosterCard (the
// #115 primitive) whose status badge over the art is the KIND badge
// (TV / Movies / Music / Books), so every cell announces its kind. The
// Movies cell carries the real TMDB poster; the other kinds use the
// initials stand-in (their wire shapes have no art).
function GlobalSearchCell({ hit, index }: { hit: GlobalSearchHit; index: number }) {
  return (
    <PosterCard
      title={hit.title}
      image={hit.image}
      meta={hit.meta || undefined}
      status={{ label: GLOBAL_SEARCH_KIND_LABEL[hit.kind], tone: 'accent' }}
      testId={`global-search-card-${hit.kind}-${index}`}
    />
  )
}
