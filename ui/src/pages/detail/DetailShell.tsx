// Unified media detail shell (issue #120, part A).
//
// One shared detail surface for all four existing detail routes
// (/movies/:id, /tv/:id, /music/:id, /books/:id): back link, common action
// row, status line, hero (poster/backdrop area + title + kind badge +
// metadata) and the kind-specific body sections. The four pages keep their
// data loading, error/loading/retry/auth states, testids and body sections;
// the shell owns the shared chrome. The action row is a reusable slot:
// part B (issue #120) wires each detail page's Add action into it — the
// link that opens the kind's existing add flow at its demoted deep route
// (/tv/add, /movies/add, /music/add, /books/add).
//
// Art: no existing endpoint serves poster/backdrop images for these kinds,
// so the poster renders an accessible decorative initials placeholder.
// `posterUrl` is supported (and falls back to the placeholder when the image
// fails) so a future art endpoint needs no shell changes.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { posterText } from '../../components/poster'
import { Badge, Toolbar } from '../../components/ui'
import type { ReactNode } from 'react'

export type DetailKind = 'movie' | 'tv' | 'music' | 'book'

export const KIND_LABEL: Record<DetailKind, string> = {
  movie: 'Movie',
  tv: 'TV',
  music: 'Artist',
  book: 'Author',
}

export interface DetailNavButton {
  testId: string
  icon: string
  label: string
  onClick: () => void
}

export interface DetailShellProps {
  kind: DetailKind
  /** Outermost page testid (e.g. 'movie-detail'); omitted in loading/error states, as before. */
  pageTestId: string
  /** True while the page data has not loaded yet. */
  loading: boolean
  loadingText: string
  loadingTestId: string
  /** Load failure message while loading; rendered as the error card. */
  error: string | null
  errorTitle: string
  errorTestId: string
  errorTestIdMessage: string
  /** True when the failure was a 401: the error card offers re-login. */
  authLost?: boolean
  onRetry: () => void
  /** Back link into the kind's library page. */
  back: { to: string; label: string; testId: string }
  /** Common action row (left / right toolbar groups). */
  actionsLeft?: ReactNode
  actionsRight?: ReactNode
  /** Pipeline/status message shown under the action row. */
  status?: string | null
  statusTestId: string
  /** Item name (poster placeholder source when no art is served). */
  title: string
  /** Heading content; defaults to `title` (movies append the year). */
  heading?: ReactNode
  /**
   * Heading testid. Defaults to the shared 'detail-hero-title'; pages with an
   * existing e2e heading testid keep it (author-detail-name,
   * artist-detail-name).
   */
  headingTestId?: string
  /** Poster image URL; '' / null / undefined = initials placeholder. */
  posterUrl?: string | null
  /** Poster testid; defaults to the shared 'detail-hero-poster'. Pages with
   * an existing poster testid keep it (movie-detail-poster,
   * author-detail-poster). */
  posterTestId?: string
  /** Existing per-page hero testid (movie-detail-header, series-detail-header, artist-detail-banner, author-detail-header). */
  heroTestId: string
  /** Prev/next library paging arrows. */
  nav?: { prev?: DetailNavButton; next?: DetailNavButton }
  /** Inline title-row extras (e.g. the author monitoring toggle). */
  titleExtras?: ReactNode
  /** Kind metadata (badges, scores, field grids, pills) under the title. */
  meta?: ReactNode
  /** Kind body sections (TV seasons, albums, books accordion, files…). */
  children: ReactNode
}

// DetailPoster is the hero's poster area: the art when a URL is supplied,
// otherwise (or after a failed load) the two-initials placeholder. It is
// decorative (aria-hidden) — the title is announced by the hero heading.
export function DetailPoster({
  title,
  url,
  testId = 'detail-hero-poster',
}: {
  title: string
  url?: string | null
  testId?: string
}) {
  const [failed, setFailed] = useState(false)
  const showImage = !!url && !failed
  return (
    <div className="detail-hero-poster" data-testid={testId} aria-hidden="true">
      {showImage ? (
        <img src={url} alt="" onError={() => setFailed(true)} />
      ) : (
        <span className="detail-hero-poster-initials">{posterText(title)}</span>
      )}
    </div>
  )
}

export function DetailShell({
  kind,
  pageTestId,
  loading,
  loadingText,
  loadingTestId,
  error,
  errorTitle,
  errorTestId,
  errorTestIdMessage,
  authLost,
  onRetry,
  back,
  actionsLeft,
  actionsRight,
  status,
  statusTestId,
  title,
  heading,
  headingTestId,
  posterUrl,
  posterTestId,
  heroTestId,
  nav,
  titleExtras,
  meta,
  children,
}: DetailShellProps) {
  if (loading) {
    if (error) {
      return (
        <div className="card" data-testid={errorTestId}>
          <h2>{errorTitle}</h2>
          <p className="error" data-testid={errorTestIdMessage}>
            {error}
          </p>
          {authLost && (
            <p>
              <Link to="/login">Sign in again</Link>
            </p>
          )}
          <button onClick={onRetry}>Retry</button>
        </div>
      )
    }
    return (
      <p className="dim" data-testid={loadingTestId}>
        {loadingText}
      </p>
    )
  }

  return (
    <div className="list-page" data-testid={pageTestId}>
      <div className="page-head">
        <Link to={back.to} className="ghost" data-testid={back.testId}>
          {back.label}
        </Link>
      </div>

      <div className="detail-actions" data-testid="detail-actions">
        <Toolbar left={actionsLeft} right={actionsRight} />
      </div>

      {status && (
        <p className="dim small" role="status" data-testid={statusTestId}>
          {status}
        </p>
      )}

      {/* Shared hero: backdrop area, poster, title + kind badge, metadata. */}
      <div className="detail-hero" data-testid={heroTestId}>
        <div className="detail-hero-backdrop" aria-hidden="true" />
        <div className="detail-hero-body" data-testid="detail-hero">
          <DetailPoster title={title} url={posterUrl} testId={posterTestId ?? 'detail-hero-poster'} />
          <div className="detail-hero-main">
            <div className="detail-title">
              <h2 data-testid={headingTestId ?? 'detail-hero-title'}>{heading ?? title}</h2>
              <Badge tone="teal" testId="detail-hero-kind">
                {KIND_LABEL[kind]}
              </Badge>
              {titleExtras}
              {nav?.prev && (
                <button
                  type="button"
                  className="nav-arrow"
                  title={nav.prev.label}
                  aria-label={nav.prev.label}
                  data-testid={nav.prev.testId}
                  onClick={nav.prev.onClick}
                >
                  {nav.prev.icon}
                </button>
              )}
              {nav?.next && (
                <button
                  type="button"
                  className="nav-arrow"
                  title={nav.next.label}
                  aria-label={nav.next.label}
                  data-testid={nav.next.testId}
                  onClick={nav.next.onClick}
                >
                  {nav.next.icon}
                </button>
              )}
            </div>
            {meta}
          </div>
        </div>
      </div>

      {children}
    </div>
  )
}
