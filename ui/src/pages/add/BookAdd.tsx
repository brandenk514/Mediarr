import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type {
  AddAuthorRequest,
  BookLookupResult,
  ProfilesResponse,
  RootFolder,
} from '../../api/types'
import { Badge, BadgeRow, Modal } from '../../components/ui'
import { MONITOR_POLICY_OPTIONS } from '../../components/music'
import { AddError, SearchBox, useAddSubmit, useDebouncedSearch } from '../../components/addflow'

// BookAddPage is the Readarr B2 book add flow: one combined Open Library
// lookup (authors + works in a single debounced search, kind-tagged hits)
// and an "Add New Author/Book" modal (Root Folder · Monitor · Quality
// Profile · Metadata Profile · Tags, with the start-search footer
// checkbox). The quality profile lists the seeded book quality profiles
// (Best / EPUB / Kindle / Any, GET /profiles?kind=books); the metadata
// profile lists the book metadata profiles (the profiles settings document).
// Already-added hits render as jump links:
// an author hit to /books/{author id}, a book hit to /books/{author id} of
// the work's author — never re-added. The books model is hierarchical
// (author → title), so adding a book whose author is new is two calls:
// POST /books/authors, then POST /books/authors/{id}/titles. The 7 monitor
// states (MONITOR_POLICY_OPTIONS, shared with the music add flow) decide
// which of the author's titles get monitored.
type MonitorPolicy = 'all' | 'future' | 'missing' | 'existing' | 'first' | 'last' | 'none'

export function BookAddPage() {
  const navigate = useNavigate()
  const search = useCallback((q: string) => client.lookupBooks(q), [])
  const { text, setText, state } = useDebouncedSearch<BookLookupResult>(search)

  // The B2 add modal: a kind-tagged hit plus the form fields it opens on.
  const [selected, setSelected] = useState<BookLookupResult | null>(null)

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="book-add-title">Add book</h2>
        <Link className="ghost" to="/books" data-testid="book-add-back">
          ← All books
        </Link>
      </div>

      <SearchBox
        label="Search authors or books (Open Library)"
        placeholder="eg. Ursula K. Le Guin, The Dispossessed"
        text={text}
        onText={setText}
        state={state}
        testId="book-search"
      />

      {state.phase === 'results' && (
        <ul className="result-list" data-testid="book-results">
          {state.hits.length === 0 && (
            <li>
              <p className="dim" data-testid="book-results-empty">
                No results — try a different term.
              </p>
            </li>
          )}
          {state.hits.map((hit, i) => (
            <BookResultRow key={`${hit.kind}-${i}`} hit={hit} index={i} onPick={setSelected} />
          ))}
        </ul>
      )}

      {selected && (
        <AddBookModal
          hit={selected}
          onClose={() => setSelected(null)}
          onAdded={() => navigate('/books')}
        />
      )}
    </div>
  )
}

// BookResultRow is one kind-tagged lookup hit. A small badge distinguishes
// author hits from book hits (book rows show the year; the work's author as
// its overview line). An already-added hit renders as a jump link to its
// detail page instead of opening the add modal.
function BookResultRow({ hit, index, onPick }: { hit: BookLookupResult; index: number; onPick: (h: BookLookupResult) => void }) {
  const isAuthor = hit.kind === 'author'
  const testId = `book-result-${hit.kind}-${index}`
  const overview = !isAuthor && hit.author ? `by ${hit.author}` : ''

  if (hit.added) {
    // Author hits jump to the author's own detail; book hits jump to the
    // detail of the work's author (both are /books/{author id}).
    const to = isAuthor
      ? hit.library_id && hit.library_id > 0
        ? `/books/${hit.library_id}`
        : '/books'
      : hit.author_id && hit.author_id > 0
        ? `/books/${hit.author_id}`
        : '/books'
    return (
      <li>
        <Link className="tv-result-row" to={to} data-testid={testId}>
          <span className="tv-result-poster" aria-hidden>
            {bookInitials(hit.name)}
          </span>
          <span className="tv-result-main">
            <span className="result-title">
              {hit.name}
              {!isAuthor && hit.year && hit.year > 0 ? (
                <span className="dim"> ({hit.year})</span>
              ) : null}
            </span>
            <BadgeRow>
              <Badge tone={isAuthor ? 'teal' : 'default'} testId={`${testId}-kind`}>
                {isAuthor ? 'Author' : 'Book'}
              </Badge>
              <Badge tone="ok" testId={`${testId}-added`}>
                In library
              </Badge>
            </BadgeRow>
            {overview && <span className="tv-result-overview dim small">{overview}</span>}
          </span>
        </Link>
      </li>
    )
  }

  return (
    <li>
      <div
        className="tv-result-row"
        role="button"
        tabIndex={0}
        onClick={() => onPick(hit)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onPick(hit)
          }
        }}
        data-testid={testId}
      >
        <span className="tv-result-poster" aria-hidden>
          {bookInitials(hit.name)}
        </span>
        <span className="tv-result-main">
          <span className="result-title">
            {hit.name}
            {!isAuthor && hit.year && hit.year > 0 ? <span className="dim"> ({hit.year})</span> : null}
          </span>
          <BadgeRow>
            <Badge tone={isAuthor ? 'teal' : 'default'} testId={`${testId}-kind`}>
              {isAuthor ? 'Author' : 'Book'}
            </Badge>
          </BadgeRow>
          {overview && <span className="tv-result-overview dim small">{overview}</span>}
        </span>
      </div>
    </li>
  )
}

// AddBookModal is the B2 "Add New Author/Book" modal — the ArtistAdd layout
// with the books field set: Root Folder → Monitor (the 7 states) → Quality
// Profile → Metadata Profile (B3: both live selects) → Tags, with the
// start-search checkbox + green Add button in the footer.
function AddBookModal({
  hit,
  onClose,
  onAdded,
}: {
  hit: BookLookupResult
  onClose: () => void
  onAdded: () => void
}) {
  const [rootFolders, setRootFolders] = useState<RootFolder[] | null>(null)
  const [rootFolderPath, setRootFolderPath] = useState('')
  const [monitorPolicy, setMonitorPolicy] = useState<MonitorPolicy>('all')
  const [profile, setProfile] = useState('')
  const [profiles, setProfiles] = useState<ProfilesResponse | null>(null)
  // Readarr B3: the metadata profile name (the seeded "Default" book
  // metadata profile and any user-created ones). Empty = none (server
  // default).
  const [metadataProfile, setMetadataProfile] = useState('')
  const [metadataProfiles, setMetadataProfiles] = useState<string[] | null>(null)
  const [tags, setTags] = useState<string[]>([])
  const [tagDraft, setTagDraft] = useState('')
  const [startSearch, setStartSearch] = useState(false)

  useEffect(() => {
    let cancelled = false
    client
      .listRootFolders()
      .then((rfs) => {
        if (cancelled) return
        setRootFolders(rfs)
        setRootFolderPath((p) => p || rfs[0]?.Path || '')
      })
      .catch(() => !cancelled && setRootFolders([]))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    client
      .getProfiles('books')
      .then((p) => {
        if (cancelled) return
        setProfiles(p)
        setProfile((v) => v || p.default || '')
      })
      .catch(() => {
        /* an empty profile list still sends '' (server default) */
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Readarr B3: load the book metadata profile names for the select. A
  // failed load leaves the list null — the select renders its loading
  // placeholder and '' is still sent (server default), matching the quality
  // profile's tolerance.
  useEffect(() => {
    let cancelled = false
    client
      .listBookMetadataProfiles()
      .then((names) => {
        if (!cancelled) setMetadataProfiles(names)
      })
      .catch(() => {
        if (!cancelled) setMetadataProfiles([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  // buildAuthorRequest assembles the full AddAuthorRequest from the modal
  // fields. Empty tags / profile / metadata profile / root folder are omitted
  // (server defaults); start_search is the server-side best-effort pipeline
  // kick, so the UI never fires a pipeline on the fresh-author path itself.
  function buildAuthorRequest(name: string): AddAuthorRequest {
    const req: AddAuthorRequest = {
      name,
      monitor_policy: monitorPolicy,
      start_search: startSearch,
    }
    if (rootFolderPath) req.root_folder = rootFolderPath
    if (profile) req.quality_profile = profile
    if (metadataProfile) req.metadata_profile = metadataProfile
    if (tags.length > 0) req.tags = tags
    return req
  }

  const { busy, error, submit } = useAddSubmit(async () => {
    if (hit.kind === 'author') {
      await client.addAuthor(buildAuthorRequest(hit.name))
      onAdded()
      return
    }
    // Book hit: the title is monitored on add (the per-title monitoring
    // toggle lives on the detail page). A work with an unknown author falls
    // back to being catalogued under a same-named author row.
    if (hit.author_id && hit.author_id > 0) {
      // The author is already in the library: add the title directly.
      await client.addTitle(hit.author_id, { name: hit.name, monitored: true })
      // The start-search checkbox has no add endpoint to ride on here, so
      // it fires the author pipeline best-effort (an artist AddArtistModal
      // pattern): the book is added even if the search fails.
      if (startSearch) {
        void client.runBooksPipeline(hit.author_id).catch(() => {
          /* best-effort */
        })
      }
    } else {
      const added = await client.addAuthor(buildAuthorRequest(hit.author || hit.name))
      await client.addTitle(added.id, { name: hit.name, monitored: true })
    }
    onAdded()
  })

  function addTag() {
    const t = tagDraft.trim().replace(/,+$/, '')
    if (!t) return
    if (!tags.includes(t)) setTags((prev) => [...prev, t])
    setTagDraft('')
  }

  const selectedFolder = (rootFolders ?? []).find((rf) => rf.Path === rootFolderPath)
  const subFolderName = hit.kind === 'author' ? hit.name : hit.author || hit.name

  return (
    <Modal
      title={hit.kind === 'author' ? 'Add New Author' : 'Add New Book'}
      onClose={onClose}
      testId="add-book-modal"
      footer={
        <>
          <label
            className="check"
            htmlFor="book-start-search"
            style={{ marginRight: 'auto' }}
          >
            <input
              id="book-start-search"
              type="checkbox"
              checked={startSearch}
              onChange={(e) => setStartSearch(e.target.checked)}
              data-testid="book-start-search"
            />
            Start search for missing books
          </label>
          <button
            type="button"
            className="add-btn-green"
            disabled={busy}
            onClick={() => {
              void submit().catch(() => {
                /* error surfaced in AddError */
              })
            }}
            data-testid="book-add-button"
          >
            {busy ? 'Adding…' : `Add ${hit.name}`}
          </button>
        </>
      }
    >
      <div className="add-artist-modal">
        {/* LEFT: artwork placeholder (no cover-art pipeline in B2) */}
        <div className="add-artist-artwork" aria-hidden data-testid="add-book-artwork">
          {bookInitials(hit.name)}
        </div>

        {/* RIGHT: name + the fields in the ArtistAdd order (minus the
            music-only Monitor New Albums, plus the B3-stub metadata profile) */}
        <div className="add-artist-fields" data-testid="book-add-form">
          <h3 data-testid="add-book-name">{hit.name}</h3>
          {hit.kind === 'book' && (
            <p className="dim small" data-testid="add-book-byline">
              {hit.author ? `by ${hit.author}` : 'Author unknown'}
              {hit.year && hit.year > 0 ? ` · ${hit.year}` : ''}
            </p>
          )}

          <label htmlFor="book-root-folder" className="dim small">
            Root Folder
          </label>
          <select
            id="book-root-folder"
            value={rootFolderPath}
            onChange={(e) => setRootFolderPath(e.target.value)}
            data-testid="add-book-root-folder"
          >
            {(rootFolders ?? []).map((rf) => (
              <option key={rf.Path} value={rf.Path}>
                {rf.Name ? `[${rf.Name}] ${rf.Path}` : rf.Path}
                {rf.FreeSpace != null ? ` · ${formatFreeSpace(rf.FreeSpace)} Free` : ''}
              </option>
            ))}
            {rootFolders !== null && rootFolders.length === 0 && <option value="">— No root folders —</option>}
          </select>
          <p className="dim small" data-testid="add-book-root-folder-help">
            '{subFolderName}' subfolder will be created automatically
            {selectedFolder?.FreeSpace != null ? ` · ${formatFreeSpace(selectedFolder.FreeSpace)} Free` : ''}
          </p>

          <label htmlFor="book-monitor-policy" className="dim small">
            Monitor
          </label>
          <select
            id="book-monitor-policy"
            value={monitorPolicy}
            onChange={(e) => setMonitorPolicy(e.target.value as MonitorPolicy)}
            data-testid="add-book-monitor-policy"
          >
            {MONITOR_POLICY_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <p className="dim small" data-testid="add-book-monitor-policy-help">
            Which books should be monitored after the author is added (one-time adjustment)
          </p>

          <label htmlFor="book-quality-profile" className="dim small">
            Quality Profile
          </label>
          <select
            id="book-quality-profile"
            value={profile}
            onChange={(e) => setProfile(e.target.value)}
            data-testid="add-book-quality-profile"
          >
            {(profiles?.profiles ?? []).map((p) => (
              <option key={p} value={p}>
                {p}
                {profiles?.default === p ? ' (default)' : ''}
              </option>
            ))}
            {profiles !== null && (profiles.profiles ?? []).length === 0 && (
              <option value="">— No profiles —</option>
            )}
          </select>

          <label htmlFor="book-metadata-profile" className="dim small">
            Metadata Profile
          </label>
          <select
            id="book-metadata-profile"
            value={metadataProfile}
            onChange={(e) => setMetadataProfile(e.target.value)}
            data-testid="add-book-metadata-profile"
          >
            <option value="">— None —</option>
            {(metadataProfiles ?? []).map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
          <p className="dim small" data-testid="add-book-metadata-profile-help">
            Which metadata filters to apply when importing this author's books
          </p>

          <label htmlFor="book-tags" className="dim small">
            Tags
          </label>
          <input
            id="book-tags"
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ',') {
                e.preventDefault()
                addTag()
              }
            }}
            onBlur={addTag}
            placeholder="Add a tag, press Enter"
            data-testid="add-book-tags-input"
          />
          {tags.length > 0 && (
            <span className="tag-chips" data-testid="add-book-tags-chips">
              {tags.map((t) => (
                <span className="tag-chip" key={t}>
                  {t}
                  <button
                    type="button"
                    aria-label={`Remove tag ${t}`}
                    onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                    data-testid={`add-book-tag-remove-${t}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </span>
          )}

          <AddError message={error} testId="book-add-error" />
        </div>
      </div>
    </Modal>
  )
}

function bookInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '📖'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}

function formatFreeSpace(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TiB`
  return `${gb.toFixed(1)} GiB`
}
