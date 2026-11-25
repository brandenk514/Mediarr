import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { client } from '../../api/client'
import type {
  AddEditionRequest,
  BookAuthorSeries,
  BookFile,
  BookHistoryEntry,
  BookSeries,
  BookWorkDetails,
  Edition,
  Author,
  ProfilesResponse,
  RootFolder,
  Title,
  UpdateAuthorRequest,
} from '../../api/types'
import { MONITOR_POLICY_OPTIONS, relTime } from '../../components/music'
import { MonitoredToggle } from '../../components/list'
import { AddError } from '../../components/addflow'
import {
  Badge,
  BadgeRow,
  Modal,
  TbButton,
  TbLink,
} from '../../components/ui'
import { resolveEditionFormat } from '../add/BookAdd'
import { DetailShell } from '../detail/DetailShell'

const DASH = '—'

function humanBytes(n: number): string {
  if (n <= 0) return DASH
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// AuthorDetailPage is the Readarr B1 author detail rendered through the
// unified detail shell (issue #120 part A): a single scrolling page — common
// action row (Add / Refresh & Scan / Search / History | Edit / Delete),
// shared hero (poster placeholder, name + monitor toggle, kind badge,
// membership badges, books/added field grid), and the books accordion. Each
// book expands to its editions — an edition is the books analogue of Radarr's
// edition model, the (format, language, monitored) triple — and below the
// editions the imported book files with their media info (pages, size,
// format, language, added date). Edit opens the Readarr B2 Edit Author modal
// (monitored · monitor policy · root folder · quality profile · metadata
// profile (B3: live select) · tags, persisted through the full author-update
// PUT); Delete is the not-yet-built stub. The Add action (issue #120 part B)
// opens the existing books add flow (/books/add, the demoted deep page).
export function AuthorDetailPage() {
  const { id } = useParams<{ id: string }>()
  const authorId = Number(id)

  const [author, setAuthor] = useState<Author | null>(null)
  const [titles, setTitles] = useState<Title[] | null>(null)
  const [series, setSeries] = useState<BookAuthorSeries[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [searching, setSearching] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [modal, setModal] = useState<null | 'history' | 'edit'>(null)
  // Readarr B2 Edit Author modal: the author's current fields, pre-filled
  // when the modal opens and persisted as a full UpdateAuthorRequest.
  const [edit, setEdit] = useState<{
    monitored: boolean
    monitorPolicy: NonNullable<UpdateAuthorRequest['monitor_policy']>
    rootFolder: string
    profile: string
    metadataProfile: string
    tags: string[]
    tagDraft: string
  } | null>(null)
  // The Edit modal's pickers load lazily when the modal opens (the music
  // Edit Artist pattern).
  const [editRootFolders, setEditRootFolders] = useState<RootFolder[] | null>(null)
  const [editProfiles, setEditProfiles] = useState<ProfilesResponse | null>(null)
  // Readarr B3: the book metadata profile names for the metadata-profile
  // select.
  const [editMetadataProfiles, setEditMetadataProfiles] = useState<string[] | null>(null)
  // Issue #120 part B: the in-detail Add Edition modal — the title it
  // targets — plus a tick that forces the expanded title's edition list
  // (TitleEditions) to refetch after a successful add.
  const [addEditionFor, setAddEditionFor] = useState<Title | null>(null)
  const [editionsTick, setEditionsTick] = useState(0)

  const reload = useCallback(() => {
    setError(null)
    Promise.all([
      client.getAuthor(authorId),
      client.listTitles(authorId),
      client.listAuthorSeries(authorId),
    ])
      .then(([a, t, s]) => {
        setAuthor(a)
        setTitles(t)
        setSeries(s)
      })
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : 'failed to load author'),
      )
  }, [authorId])

  useEffect(() => {
    reload()
  }, [reload])

  // The Edit modal's pickers load the root-folder list, the book quality
  // profiles, and the book metadata profile names lazily, only while the
  // modal is open.
  useEffect(() => {
    if (modal !== 'edit') return
    setEditRootFolders(null)
    setEditProfiles(null)
    setEditMetadataProfiles(null)
    client
      .listRootFolders()
      .then(setEditRootFolders)
      .catch(() => setEditRootFolders([]))
    client
      .getProfiles('books')
      .then(setEditProfiles)
      .catch(() => setEditProfiles(null))
    client
      .listBookMetadataProfiles()
      .then(setEditMetadataProfiles)
      .catch(() => setEditMetadataProfiles([]))
  }, [modal])

  // Search runs the author's indexer pipeline (the books analogue of Radarr's
  // "Search" command) and reports the import result.
  async function search() {
    setSearching(true)
    setStatus('Searching releases…')
    try {
      const res = await client.runBooksPipeline(authorId)
      setStatus(res.imported || 'Search complete — no new releases imported.')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'release search failed')
    } finally {
      setSearching(false)
    }
  }

  return (
    <DetailShell
      kind="book"
      pageTestId="author-detail"
      loading={!author}
      loadingText="Loading author…"
      loadingTestId="author-detail-loading"
      error={!author ? error : null}
      errorTitle="Author unavailable"
      errorTestId="author-detail-error"
      errorTestIdMessage="author-detail-error-message"
      onRetry={reload}
      back={{ to: '/books', label: '← Books', testId: 'author-detail-back' }}
      actionsLeft={
        <>
          <TbLink icon="+" label="Add" to="/books/add" testId="author-detail-add" />
          <TbButton icon="⟳" label="Refresh & Scan" testId="author-detail-refresh" onClick={() => void reload()} />
          <TbButton
            icon="🔍"
            label="Search"
            testId="author-detail-search"
            disabled={searching}
            onClick={() => void search()}
          />
          <TbButton
            icon="🕘"
            label="History"
            testId="author-detail-history"
            divider
            onClick={() => setModal('history')}
          />
        </>
      }
      actionsRight={
        <>
          <TbButton
            icon="✏"
            label="Edit"
            testId="author-detail-edit"
            onClick={() => {
              if (!author) return
              // '' is the wire form of "all" for authors added before the
              // UI offers a choice — coerce so the select shows a selected
              // option.
              setEdit({
                monitored: author.Monitored,
                monitorPolicy: author.MonitorPolicy || 'all',
                rootFolder: author.RootFolder ?? '',
                profile: author.QualityProfile ?? '',
                metadataProfile: author.MetadataProfile ?? '',
                tags: author.Tags ?? [],
                tagDraft: '',
              })
              setModal('edit')
            }}
          />
          <TbButton
            icon="🗑"
            label="Delete"
            testId="author-detail-delete"
            divider
            disabled
            title="Author deletion lands in a later phase"
          />
        </>
      }
      status={status}
      statusTestId="author-detail-status"
      title={author?.Name ?? ''}
      heading={author?.Name}
      headingTestId="author-detail-name"
      heroTestId="author-detail-header"
      posterTestId="author-detail-poster"
      titleExtras={
        author ? (
          <MonitoredToggle
            monitored={author.Monitored}
            onToggle={async (next) => {
              await client.setAuthorMonitored(authorId, next)
              setAuthor({ ...author, Monitored: next })
            }}
            label={`${author.Name} monitoring`}
            testId="author-detail-monitored"
          />
        ) : null
      }
      meta={
        author ? (
          <>
            <BadgeRow>
              <Badge tone={author.Monitored ? 'ok' : 'default'} testId="badge-monitored">
                {author.Monitored ? 'Monitored' : 'Unmonitored'}
              </Badge>
              <Badge testId="badge-book-count">
                {titles ? `${titles.length} book${titles.length === 1 ? '' : 's'}` : ''}
              </Badge>
              {series && series.length > 0
                ? series.map((s) => (
                    <Badge key={s.ID} tone="teal" testId={`badge-series-${s.ID}`}>
                      {s.Name}
                    </Badge>
                  ))
                : null}
            </BadgeRow>

            <div className="field-grid" data-testid="author-detail-fields">
              <div>
                <span className="fg-label">Books</span>
                <span className="fg-value" data-testid="field-books">
                  {titles ? titles.length : DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Added</span>
                <span className="fg-value" data-testid="field-added">
                  {author.AddedAt}
                </span>
              </div>
            </div>
          </>
        ) : null
      }
    >
      {author ? (
        <>
          <BooksAccordion
            titles={titles ?? []}
            onToggle={reload}
            editionsTick={editionsTick}
            onAddEdition={(t) => setAddEditionFor(t)}
          />

          {/* History modal (the per-author history feed; books don't flow into
              the global feed yet). */}
          {modal === 'history' && <HistoryModal authorId={authorId} onClose={() => setModal(null)} />}

          {/* Edit Author modal (Readarr B2). Persists through PUT
              /books/authors/{id} (UpdateAuthorRequest): Monitored · Monitor
              Policy · Root Folder · Quality Profile · Metadata Profile ·
              Tags. A monitored change fans out to the author's known titles
              server-side. */}
          {modal === 'edit' && edit && (
            <Modal
              title="Edit Author"
              onClose={() => setModal(null)}
              testId="edit-modal"
              footer={
                <>
                  <button type="button" className="ghost" onClick={() => setModal(null)}>
                    Cancel
                  </button>
                  <button
                    type="button"
                    data-testid="edit-save"
                    onClick={() => {
                      void (async () => {
                        await client.updateAuthor(authorId, {
                          monitored: edit.monitored,
                          monitor_policy: edit.monitorPolicy,
                          root_folder: edit.rootFolder,
                          quality_profile: edit.profile,
                          metadata_profile: edit.metadataProfile,
                          tags: edit.tags,
                        })
                        setModal(null)
                        setAuthor({ ...author, Monitored: edit.monitored })
                        setStatus('Author updated.')
                        void reload()
                      })()
                    }}
                  >
                    Save
                  </button>
                </>
              }
            >
              <label className="check" htmlFor="edit-monitored" style={{ marginBottom: '0.6rem' }}>
                <input
                  id="edit-monitored"
                  type="checkbox"
                  checked={edit.monitored}
                  onChange={(e) => setEdit({ ...edit, monitored: e.target.checked })}
                  data-testid="edit-monitored"
                />
                Monitored
              </label>
              <p className="dim small" style={{ marginTop: '0' }}>
                Monitor this author's books
              </p>

              <label style={{ display: 'block', marginBottom: '0.6rem' }}>
                <span className="dim small">Monitor Policy</span>
                <select
                  value={edit.monitorPolicy}
                  onChange={(e) =>
                    setEdit({ ...edit, monitorPolicy: e.target.value as NonNullable<UpdateAuthorRequest['monitor_policy']> })
                  }
                  data-testid="edit-monitor-policy"
                >
                  {MONITOR_POLICY_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
              <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
                Which books should be monitored (one-time adjustment)
              </p>

              <label style={{ display: 'block', marginBottom: '0.6rem' }}>
                <span className="dim small">Root Folder</span>
                <select
                  value={edit.rootFolder}
                  onChange={(e) => setEdit({ ...edit, rootFolder: e.target.value })}
                  data-testid="edit-root-folder"
                >
                  {!(editRootFolders ?? []).some((rf) => rf.Path === edit.rootFolder) && (
                    <option value={edit.rootFolder}>
                      {edit.rootFolder === '' ? '— Server default —' : edit.rootFolder}
                    </option>
                  )}
                  {(editRootFolders ?? []).map((rf) => (
                    <option key={rf.Path} value={rf.Path}>
                      {rf.Name ? `[${rf.Name}] ${rf.Path}` : rf.Path}
                    </option>
                  ))}
                </select>
              </label>
              <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
                Where this author's files are stored (empty = server default)
              </p>

              <label style={{ display: 'block', marginBottom: '0.6rem' }}>
                <span className="dim small">Quality Profile</span>
                <select
                  value={edit.profile}
                  onChange={(e) => setEdit({ ...edit, profile: e.target.value })}
                  data-testid="edit-profile"
                >
                  <option value="">— None —</option>
                  {(editProfiles?.profiles ?? []).map((p) => (
                    <option key={p} value={p}>
                      {p}
                      {editProfiles?.default === p ? ' (default)' : ''}
                    </option>
                  ))}
                  {editProfiles !== null &&
                    edit.profile !== '' &&
                    !(editProfiles.profiles ?? []).includes(edit.profile) && (
                      <option value={edit.profile}>{edit.profile}</option>
                    )}
                </select>
              </label>

              <label style={{ display: 'block', marginBottom: '0.6rem' }}>
                <span className="dim small">Metadata Profile</span>
                <select
                  value={edit.metadataProfile}
                  onChange={(e) => setEdit({ ...edit, metadataProfile: e.target.value })}
                  data-testid="edit-metadata-profile"
                >
                  <option value="">— None —</option>
                  {(editMetadataProfiles ?? []).map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                  {editMetadataProfiles !== null &&
                    edit.metadataProfile !== '' &&
                    !(editMetadataProfiles ?? []).includes(edit.metadataProfile) && (
                      <option value={edit.metadataProfile}>{edit.metadataProfile}</option>
                    )}
                </select>
              </label>
              <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
                Which metadata filters apply to this author's books (empty = server default)
              </p>

              <label style={{ display: 'block', marginBottom: '0.6rem' }}>
                <span className="dim small">Tags</span>
                <input
                  type="text"
                  value={edit.tagDraft}
                  onChange={(e) => setEdit({ ...edit, tagDraft: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ',') {
                      e.preventDefault()
                      const t = edit.tagDraft.trim().replace(/,+$/, '')
                      if (t && !edit.tags.includes(t)) setEdit({ ...edit, tags: [...edit.tags, t], tagDraft: '' })
                    }
                  }}
                  onBlur={() => {
                    const t = edit.tagDraft.trim().replace(/,+$/, '')
                    if (t && !edit.tags.includes(t)) setEdit({ ...edit, tags: [...edit.tags, t], tagDraft: '' })
                  }}
                  placeholder="Add a tag, press Enter"
                  data-testid="edit-tags-input"
                />
              </label>
              {edit.tags.length > 0 && (
                <span className="tag-chips" data-testid="edit-tags-chips">
                  {edit.tags.map((t) => (
                    <span className="tag-chip" key={t}>
                      {t}
                      <button
                        type="button"
                        aria-label={`Remove tag ${t}`}
                        onClick={() => setEdit({ ...edit, tags: edit.tags.filter((x) => x !== t) })}
                        data-testid={`edit-tag-remove-${t}`}
                      >
                        ✕
                      </button>
                    </span>
                  ))}
                </span>
              )}
            </Modal>
          )}

          {/* Issue #120 part B: in-detail Add Edition modal — provider-fed
              Open Library editions (the same lookupBooks ->
              getBookWorkEditions chain the /books/add page uses) with a
              manual fallback; posts to the existing
              POST /books/titles/{id}/editions endpoint. */}
          {addEditionFor && (
            <AddEditionModal
              title={addEditionFor}
              authorName={author.Name}
              onClose={() => setAddEditionFor(null)}
              onAdded={(count) => {
                setStatus(
                  count === 1
                    ? `Added 1 edition to "${addEditionFor.Name}".`
                    : `Added ${count} editions to "${addEditionFor.Name}".`,
                )
                setEditionsTick((n) => n + 1)
                setAddEditionFor(null)
              }}
              onFailed={(message) => setStatus(message)}
            />
          )}
        </>
      ) : null}
    </DetailShell>
  )
}

// BooksAccordion is the expandable books list. Each book row expands to the
// title monitor toggle + series-membership badges, the edition list (the
// (format, language, monitored) triples), and the imported book files with
// their media info.
function BooksAccordion({
  titles,
  onToggle,
  editionsTick,
  onAddEdition,
}: {
  titles: Title[]
  onToggle: () => void
  editionsTick: number
  onAddEdition: (title: Title) => void
}) {
  const [open, setOpen] = useState<ReadonlySet<number>>(new Set())

  function toggle(id: number) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (titles.length === 0)
    return (
      <div className="card" data-testid="author-detail-books-empty">
        <h3>Books</h3>
        <p className="dim">No books yet. Add one from the library page.</p>
      </div>
    )

  return (
    <div data-testid="author-detail-books">
      {titles.map((t) => {
        const expanded = open.has(t.ID)
        return (
          <div className="type-group" key={t.ID} data-testid={`book-accordion-${t.ID}`}>
            <div className="type-group-head">
              <button
                type="button"
                aria-expanded={expanded}
                onClick={() => toggle(t.ID)}
                data-testid={`book-expand-${t.ID}`}
                style={{
                  border: 'none',
                  background: 'none',
                  cursor: 'pointer',
                  color: 'var(--dim)',
                  fontSize: '0.9rem',
                  padding: 0,
                }}
              >
                {expanded ? '▾' : '▸'}
              </button>
              <span>{t.Name}</span>
              <span className="tg-size" data-testid={`book-added-${t.ID}`}>
                {t.AddedAt}
              </span>
              <MonitoredToggle
                monitored={t.Monitored}
                onToggle={async (next) => {
                  await client.setTitleMonitored(t.ID, next)
                  onToggle()
                }}
                label={`${t.Name} monitoring`}
                testId={`book-mon-${t.ID}`}
              />
            </div>

            {expanded && (
              <div className="album-sub" data-testid={`book-detail-${t.ID}`}>
                <TitleSeries titleId={t.ID} />
                <TitleEditions
                  titleId={t.ID}
                  onToggle={onToggle}
                  editionsTick={editionsTick}
                  onAddEdition={() => onAddEdition(t)}
                />
                <TitleFiles titleId={t.ID} />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// TitleSeries shows the series-membership badges for a book (the many-to-many
// book↔series link surface) plus the Readarr B4 "add to series" control:
// link the book into an existing series (POST /books/titles/{id}/series) or
// name a new one (POST /books/series, idempotent by name, then link).
function TitleSeries({ titleId }: { titleId: number }) {
  const [list, setList] = useState<BookSeries[] | null>(null)
  const [refresh, setRefresh] = useState(0)

  useEffect(() => {
    client
      .listTitleSeries(titleId)
      .then(setList)
      .catch(() => setList([]))
  }, [titleId, refresh])

  return (
    <span
      data-testid={`book-series-${titleId}`}
      style={{ display: 'inline-flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}
    >
      {(list ?? []).map((s) => (
        <Badge key={s.ID} tone="teal" testId={`book-series-badge-${s.ID}`}>
          {s.Name}
        </Badge>
      ))}
      <AddToSeries titleId={titleId} onLinked={() => setRefresh((k) => k + 1)} />
    </span>
  )
}

// AddToSeries is the link-a-title control inside the book detail: a pick of
// the existing series, or a new series name (created idempotently first),
// then the title↔series link.
function AddToSeries({ titleId, onLinked }: { titleId: number; onLinked: () => void }) {
  const [allSeries, setAllSeries] = useState<BookSeries[] | null>(null)
  const [selected, setSelected] = useState(0)
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    client
      .listBookSeries()
      .then(setAllSeries)
      .catch(() => setAllSeries([]))
  }, [])

  const canLink =
    newName.trim() !== '' || (allSeries !== null && allSeries.some((s) => s.ID === selected))

  async function link() {
    if (busy || !allSeries) return
    setBusy(true)
    setError(null)
    try {
      let seriesId = 0
      const trimmed = newName.trim()
      if (trimmed) {
        // A new name creates the series first (idempotent by name), then
        // links the book into it.
        const created = await client.addBookSeries({ name: trimmed })
        seriesId = created.id
      } else {
        const s = allSeries.find((x) => x.ID === selected)
        if (!s) return
        seriesId = s.ID
      }
      await client.linkTitleToSeries(titleId, seriesId)
      setNewName('')
      setSelected(0)
      onLinked()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'failed to add to series')
    } finally {
      setBusy(false)
    }
  }

  return (
    <span
      data-testid={`book-series-add-${titleId}`}
      style={{ display: 'inline-flex', gap: '0.35rem', alignItems: 'center', flexWrap: 'wrap' }}
    >
      <select
        value={selected}
        onChange={(e) => setSelected(Number(e.target.value))}
        aria-label={`Series for this book`}
        data-testid={`book-series-add-select-${titleId}`}
      >
        {(allSeries ?? []).map((s) => (
          <option key={s.ID} value={s.ID}>
            {s.Name}
          </option>
        ))}
      </select>
      <input
        value={newName}
        onChange={(e) => setNewName(e.target.value)}
        placeholder="New series name"
        aria-label="New series name"
        data-testid={`book-series-add-name-${titleId}`}
      />
      <button
        type="button"
        onClick={link}
        disabled={busy || !canLink}
        data-testid={`book-series-add-btn-${titleId}`}
      >
        Add
      </button>
      {error && (
        <span className="error" data-testid={`book-series-add-error-${titleId}`}>
          {error}
        </span>
      )}
    </span>
  )
}

// TitleEditions lists a book's editions. Each edition carries the B1 triple —
// format (EPUB/MOBI/AZW3), language, and a per-edition monitored flag.
function TitleEditions({
  titleId,
  onToggle,
  editionsTick,
  onAddEdition,
}: {
  titleId: number
  onToggle: () => void
  editionsTick: number
  onAddEdition: () => void
}) {
  const [editions, setEditions] = useState<Edition[] | null>(null)

  // editionsTick bumps after an in-detail add so the freshly added edition
  // shows up without leaving the page (issue #120 part B).
  useEffect(() => {
    client
      .listEditions(titleId)
      .then(setEditions)
      .catch(() => setEditions([]))
  }, [titleId, editionsTick])

  if (!editions) return null

  return (
    <div data-testid={`book-editions-${titleId}`}>
      <div
        style={{ display: 'flex', alignItems: 'baseline', gap: '0.5rem', marginBottom: '0.5rem' }}
      >
        <h4 style={{ margin: 0 }}>Editions</h4>
        <button type="button" onClick={onAddEdition} data-testid={`book-add-edition-${titleId}`}>
          Add Edition
        </button>
      </div>
      {editions.length === 0 ? (
        <p className="dim" data-testid={`book-editions-empty-${titleId}`}>
          No editions yet. Use Add Edition to fetch candidates from Open
          Library or enter one manually.
        </p>
      ) : (
        editions.map((e) => (
          <div className="edition-row" key={e.ID} data-testid={`edition-${e.ID}`}>
            <span className="ed-label">
              <Badge tone="teal">{e.Format.toUpperCase()}</Badge>
            </span>
            <span className="ed-meta">
              {e.Language || DASH}
              {e.Year > 0 ? ` · ${e.Year}` : ''}
              {e.Pages > 0 ? ` · ${e.Pages} pp` : ''}
              {e.ISBN ? ` · ISBN ${e.ISBN}` : ''}
            </span>
            <MonitoredToggle
              monitored={e.Monitored}
              onToggle={async (next) => {
                await client.setEditionMonitored(e.ID, next)
                onToggle()
              }}
              label={`${e.Format} edition monitoring`}
              testId={`edition-mon-${e.ID}`}
            />
          </div>
        ))
      )}
    </div>
  )
}

// TitleFiles is the book-files table: the imported e-book files with their
// media info (pages, size, format, language, added date).
function TitleFiles({ titleId }: { titleId: number }) {
  const [files, setFiles] = useState<BookFile[] | null>(null)

  useEffect(() => {
    client
      .listBookFiles(titleId)
      .then(setFiles)
      .catch(() => setFiles([]))
  }, [titleId])

  if (!files) return null

  return (
    <div data-testid={`book-files-${titleId}`}>
      <h4>Files</h4>
      {files.length === 0 ? (
        <p className="dim" data-testid={`book-files-empty-${titleId}`}>
          No book files yet.
        </p>
      ) : (
        <table className="import-table" data-testid={`book-files-table-${titleId}`}>
          <thead>
            <tr>
              <th>Path</th>
              <th>Format</th>
              <th>Language</th>
              <th>Pages</th>
              <th>Size</th>
              <th>Added</th>
            </tr>
          </thead>
          <tbody>
            {files.map((f, i) => (
              <tr key={f.ID} data-testid={`book-file-row-${i}`}>
                <td>{f.Path}</td>
                <td>{f.Format || DASH}</td>
                <td>{f.Language || DASH}</td>
                <td>{f.Pages > 0 ? f.Pages : DASH}</td>
                <td>{humanBytes(f.Size)}</td>
                <td>{f.AddedAt}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// HistoryModal is the per-author history feed (the books analogue of the
// shared P3 history surface; books don't flow into the global feed yet).
function HistoryModal({ authorId, onClose }: { authorId: number; onClose: () => void }) {
  const [entries, setEntries] = useState<BookHistoryEntry[] | null>(null)

  useEffect(() => {
    client
      .listAuthorHistory(authorId)
      .then(setEntries)
      .catch(() => setEntries([]))
  }, [authorId])

  return (
    <Modal title="History" onClose={onClose} testId="history-modal">
      {entries === null ? (
        <p className="dim">Loading history…</p>
      ) : entries.length === 0 ? (
        <p className="dim" data-testid="author-history-empty">
          No history yet.
        </p>
      ) : (
        entries.map((h, i) => (
          <div className="history-row" key={h.ID} data-testid={`history-${i}`}>
            <span className="hr-event">{h.Event}</span>
            <span className="hr-detail">{h.Detail}</span>
            <span className="hr-at">{relTime(h.At)}</span>
          </div>
        ))
      )}
    </Modal>
  )
}

// AddEditionModal is the in-detail Add Edition flow (issue #120 part B):
// the same Open Library chain the /books/add page uses — lookupBooks
// resolves the title to an OL work key, getBookWorkEditions fetches the
// provider-fed edition candidates — with a manual fallback form when the
// provider can't offer editions (no work key, a fetch failure, or an empty
// work) so the flow never dead-ends. Each picked edition is created through
// the existing POST /books/titles/{id}/editions endpoint (client.addEdition).
function AddEditionModal({
  title,
  authorName,
  onClose,
  onAdded,
  onFailed,
}: {
  title: Title
  authorName: string
  onClose: () => void
  onAdded: (count: number) => void
  onFailed: (message: string) => void
}) {
  const [phase, setPhase] = useState<'loading' | 'provider' | 'manual'>('loading')
  const [details, setDetails] = useState<BookWorkDetails | null>(null)
  // Per-edition monitored picks for the provider list; a missing entry means
  // true (checked by default) — the BookAdd convention.
  const [monitoredSel, setMonitoredSel] = useState<Record<string, boolean>>({})
  // The manual fallback edition form.
  const [mFormat, setMFormat] = useState<AddEditionRequest['format']>('epub')
  const [mLanguage, setMLanguage] = useState('')
  const [mIsbn, setMIsbn] = useState('')
  const [mPublisher, setMPublisher] = useState('')
  const [mYear, setMYear] = useState('')
  const [mMonitored, setMMonitored] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Resolve the title's work key via the Open Library lookup (the same
  // lookupBooks call the /books/add page debounces against), then fetch the
  // work's editions. A same-named work by this author wins ties.
  useEffect(() => {
    let cancelled = false
    client
      .lookupBooks(title.Name)
      .then((hits) => {
        if (cancelled) return
        const want = title.Name.trim().toLowerCase()
        const candidates = hits.filter(
          (h) =>
            h.kind === 'book' &&
            h.name.trim().toLowerCase() === want &&
            h.work_key &&
            /^ol\d+w$/i.test(h.work_key.trim()),
        )
        const hit =
          candidates.find(
            (h) => (h.author ?? '').trim().toLowerCase() === authorName.trim().toLowerCase(),
          ) ?? candidates[0]
        const key = hit?.work_key?.trim().toUpperCase() ?? null
        if (!key) {
          setPhase('manual')
          return
        }
        client
          .getBookWorkEditions(key)
          .then((d) => {
            if (cancelled) return
            if ((d.editions ?? []).length === 0) {
              setPhase('manual')
              return
            }
            setDetails(d)
            setPhase('provider')
          })
          .catch(() => {
            if (!cancelled) setPhase('manual')
          })
      })
      .catch(() => {
        if (!cancelled) setPhase('manual')
      })
    return () => {
      cancelled = true
    }
  }, [title.Name, authorName])

  // buildRequests assembles the AddEditionRequest payloads: the picked
  // provider editions (an unmonitorable edition — no format in the
  // quality-profile set — is skipped, its toggle being disabled) or the
  // manual form's single edition. Empty fields are omitted (server
  // defaults), mirroring BookAdd's buildEditionRequests.
  function buildRequests(): AddEditionRequest[] {
    if (phase === 'provider' && details) {
      const reqs: AddEditionRequest[] = []
      for (const e of details.editions) {
        if (monitoredSel[e.key] === false) continue
        const format = resolveEditionFormat(e.formats)
        if (!format) continue
        const req: AddEditionRequest = { format, monitored: true }
        const language = e.languages?.[0]
        if (language) req.language = language
        if (e.isbn) req.isbn = e.isbn
        const publisher = e.publishers?.[0]
        if (publisher) req.publisher = publisher
        if (e.year && e.year > 0) req.year = e.year
        if (e.pages && e.pages > 0) req.pages = e.pages
        reqs.push(req)
      }
      return reqs
    }
    if (!mMonitored) return []
    const req: AddEditionRequest = { format: mFormat, monitored: true }
    const language = mLanguage.trim()
    if (language) req.language = language
    const isbn = mIsbn.trim()
    if (isbn) req.isbn = isbn
    const publisher = mPublisher.trim()
    if (publisher) req.publisher = publisher
    const year = Number(mYear.trim())
    if (mYear.trim() !== '' && Number.isInteger(year) && year > 0) req.year = year
    return [req]
  }

  async function submit() {
    if (busy) return
    const reqs = buildRequests()
    if (reqs.length === 0) return
    setBusy(true)
    setError(null)
    try {
      for (const req of reqs) {
        await client.addEdition(title.ID, req)
      }
      onAdded(reqs.length)
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : 'failed to add edition'
      setError(message)
      onFailed(message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Add Edition"
      onClose={onClose}
      testId="add-edition-modal"
      footer={
        <>
          <button type="button" className="ghost" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            disabled={busy || buildRequests().length === 0}
            onClick={() => {
              void submit()
            }}
            data-testid="add-edition-button"
          >
            {busy ? 'Adding…' : 'Add Edition'}
          </button>
        </>
      }
    >
      <p className="dim small" data-testid="add-edition-title">
        {title.Name}
        {authorName ? ` · ${authorName}` : ''}
      </p>

      {phase === 'loading' && (
        <p className="dim" data-testid="add-edition-loading">
          Looking up Open Library editions…
        </p>
      )}

      {phase === 'provider' && details && (
        <div data-testid="add-edition-provider-list">
          {details.editions.map((e) => {
            const monitorable = resolveEditionFormat(e.formats) !== null
            return (
              <div className="edition-row" key={e.key} data-testid={`add-edition-provider-${e.key}`}>
                <span className="ed-label">{e.title || '—'}</span>
                <span className="ed-meta dim">
                  {e.formats && e.formats.length > 0 ? e.formats.join(', ') : '—'}
                  {e.languages && e.languages.length > 0 ? ` · ${e.languages.join(', ')}` : ''}
                  {e.publishers && e.publishers.length > 0 ? ` · ${e.publishers[0]}` : ''}
                  {e.year && e.year > 0 ? ` · ${e.year}` : ''}
                  {e.isbn ? ` · ISBN ${e.isbn}` : ''}
                  {!monitorable ? ' · no monitorable format' : ''}
                </span>
                <label className="check" htmlFor={`add-edition-provider-mon-${e.key}`}>
                  <input
                    id={`add-edition-provider-mon-${e.key}`}
                    type="checkbox"
                    checked={monitoredSel[e.key] ?? true}
                    disabled={!monitorable}
                    onChange={(ev) =>
                      setMonitoredSel((prev) => ({ ...prev, [e.key]: ev.target.checked }))
                    }
                    data-testid={`add-edition-provider-${e.key}-mon`}
                  />
                  Monitor
                </label>
              </div>
            )
          })}
        </div>
      )}

      {phase === 'manual' && (
        <div data-testid="add-edition-manual">
          <p className="dim small">
            Open Library has no editions for this title — enter one manually.
          </p>
          <label htmlFor="add-edition-format" className="dim small">
            Format
          </label>
          <select
            id="add-edition-format"
            value={mFormat}
            onChange={(e) => setMFormat(e.target.value as AddEditionRequest['format'])}
            data-testid="add-edition-format"
          >
            <option value="epub">EPUB</option>
            <option value="mobi">MOBI</option>
            <option value="azw3">AZW3</option>
          </select>
          <label htmlFor="add-edition-language" className="dim small">
            Language
          </label>
          <input
            id="add-edition-language"
            value={mLanguage}
            onChange={(e) => setMLanguage(e.target.value)}
            placeholder="en (optional)"
            data-testid="add-edition-language"
          />
          <label htmlFor="add-edition-isbn" className="dim small">
            ISBN
          </label>
          <input
            id="add-edition-isbn"
            value={mIsbn}
            onChange={(e) => setMIsbn(e.target.value)}
            placeholder="978… (optional)"
            data-testid="add-edition-isbn"
          />
          <label htmlFor="add-edition-publisher" className="dim small">
            Publisher
          </label>
          <input
            id="add-edition-publisher"
            value={mPublisher}
            onChange={(e) => setMPublisher(e.target.value)}
            placeholder="Publisher (optional)"
            data-testid="add-edition-publisher"
          />
          <label htmlFor="add-edition-year" className="dim small">
            Year
          </label>
          <input
            id="add-edition-year"
            type="number"
            min={0}
            value={mYear}
            onChange={(e) => setMYear(e.target.value)}
            placeholder="e.g. 2018 (optional)"
            data-testid="add-edition-year"
          />
          <label className="check" htmlFor="add-edition-monitored">
            <input
              id="add-edition-monitored"
              type="checkbox"
              checked={mMonitored}
              onChange={(e) => setMMonitored(e.target.checked)}
              data-testid="add-edition-monitored"
            />
            Monitor this edition
          </label>
        </div>
      )}

      {error && <AddError message={error} testId="add-edition-error" />}
    </Modal>
  )
}
