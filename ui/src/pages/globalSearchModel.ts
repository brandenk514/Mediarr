// Global search (issue #117) — the pure model + per-kind field mapping that
// the results page (GlobalSearch.tsx) renders.
//
// Each mapper takes the REAL per-kind wire shape (see ui/src/api/types.gen.ts)
// and returns unified cells. No poster URLs are invented: only MovieSearchHit
// carries one; TV / Music / Books use the PosterCard initials stand-in.

import { ApiError } from '../api/client'
import type {
  ArtistSearchResult,
  BookLookupResult,
  MovieSearchHit,
  SearchResult,
} from '../api/types'

export type GlobalSearchKind = 'tv' | 'movie' | 'music' | 'book'

/** One normalized result cell in the unified grid, regardless of kind. */
export interface GlobalSearchHit {
  kind: GlobalSearchKind
  title: string
  /** Second line under the title (year · runtime · rating, author, country…). */
  meta: string
  /** Poster / cover URL. Only the Movies hit shape carries one; the other
   *  kinds fall back to the PosterCard initials stand-in. */
  image?: string
}

/** Fixed render order of the kinds in the unified grid. */
export const GLOBAL_SEARCH_KINDS: GlobalSearchKind[] = ['tv', 'movie', 'music', 'book']

/** Human kind labels rendered on each result's badge. */
export const GLOBAL_SEARCH_KIND_LABEL: Record<GlobalSearchKind, string> = {
  tv: 'TV',
  movie: 'Movies',
  music: 'Music',
  book: 'Books',
}

export function mapMovieHits(rows: MovieSearchHit[]): GlobalSearchHit[] {
  return rows.map((h) => ({
    kind: 'movie',
    title: h.title,
    meta: [h.year ? String(h.year) : '', h.certification, h.runtime > 0 ? `${h.runtime}m` : '']
      .filter(Boolean)
      .join(' · '),
    image: h.poster || undefined,
  }))
}

export function mapTvHits(rows: SearchResult[]): GlobalSearchHit[] {
  return rows.map((r) => ({
    kind: 'tv',
    title: r.title,
    meta: r.year ? String(r.year) : '',
  }))
}

// The artists list is the kind's identity (release groups are album-level
// hits for the Add Artist flow); the unified grid lists the artists.
export function mapMusicHits(result: ArtistSearchResult): GlobalSearchHit[] {
  return (result?.artists ?? []).map((a) => ({
    kind: 'music',
    title: a.title,
    meta: a.country || a.disambiguation || '',
  }))
}

// Both lookup hit kinds land in the grid: author hits (the name IS the
// author) and book hits (work title + its author / year).
export function mapBookHits(rows: BookLookupResult[]): GlobalSearchHit[] {
  return rows.map((b) =>
    b.kind === 'book'
      ? {
          kind: 'book',
          title: b.name,
          meta: [b.author, b.year ? String(b.year) : ''].filter(Boolean).join(' · '),
        }
      : {
          kind: 'book',
          title: b.name,
          meta: 'Author',
        },
  )
}

// ---- per-kind fan-out state ------------------------------------------------

/** One kind's fan-out phase. A kind is independently loading / done / error. */
export type GlobalSearchPhase = 'idle' | 'loading' | 'done' | 'error'

export interface KindFanoutState {
  phase: GlobalSearchPhase
  hits: GlobalSearchHit[]
  /** Failure message (503 "provider not configured", upstream 502, …). */
  error?: string
}

const IDLE: KindFanoutState = { phase: 'idle', hits: [] }

export function freshFanoutStates(): Record<GlobalSearchKind, KindFanoutState> {
  return { tv: IDLE, movie: IDLE, music: IDLE, book: IDLE }
}

export function fanoutMessage(e: unknown): string {
  if (e instanceof ApiError) return `${e.status}: ${e.message}`
  if (e instanceof Error) return e.message
  return 'search failed'
}
