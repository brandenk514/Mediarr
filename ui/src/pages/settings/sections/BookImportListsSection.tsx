import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type {
  BooksBookImportList,
  BooksBookImportListExclusion,
  BooksBookImportListsSettings,
} from '../../../api/books-b5'
import { BOOK_IMPORT_LIST_KINDS } from '../../../api/books-b5'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// BookImportListsSection is the Readarr B5 books import-lists section:
// Goodreads lists (list id + site base URL), LazyLibrarian instances and
// Readarr-instance bridges (base URL + masked API key), the Readarr bridge's
// source-side filters (profiles / tags / root folders), and the document-level
// exclusions (books no list should ever import). API keys are masked by the
// server — a blank field keeps the stored key. There is no run action: the
// books importer has no /import endpoint yet (the movie/TV one reads the
// shared import-lists document, not this one).
//
// Wire shapes come from books-b5.ts: the backend's BookImportList diverges
// from openapi.yaml (int list_id + enabled + document-level exclusions).
const GOODREADS_DEFAULT_URL = 'https://www.goodreads.com'

// parseIds parses a comma-separated id field into a de-duplicated int list
// (blank entries / non-numeric tokens are dropped).
function parseIds(raw: string): number[] {
  const out: number[] = []
  for (const part of raw.split(',')) {
    const t = part.trim()
    if (t === '') continue
    const n = Number(t)
    if (Number.isInteger(n) && !out.includes(n)) out.push(n)
  }
  return out
}

// parsePaths parses a comma-separated path field (trimmed, blanks dropped).
function parsePaths(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
}

export function BookImportListsSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<BooksBookImportListsSettings>(
    'book-import-lists',
  )
  const [lists, setLists] = useState<BooksBookImportList[]>([])
  const [exclusions, setExclusions] = useState<BooksBookImportListExclusion[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  // Draft form state.
  const [editId, setEditId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<(typeof BOOK_IMPORT_LIST_KINDS)[number]>('goodreads')
  const [baseUrl, setBaseUrl] = useState('')
  const [listId, setListId] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [profileIds, setProfileIds] = useState('')
  const [tagIds, setTagIds] = useState('')
  const [rootPaths, setRootPaths] = useState('')
  // Exclusion draft.
  const [excForeign, setExcForeign] = useState('')
  const [excName, setExcName] = useState('')

  useEffect(() => {
    if (doc) {
      setLists(doc.lists ?? [])
      setExclusions(doc.exclusions ?? [])
    }
  }, [doc])

  if (error) {
    return (
      <SectionError message={error} authLost={authLost} onRetry={refresh} testId="blb-error" />
    )
  }

  function beginEdit(l: BooksBookImportList) {
    setEditId(l.id)
    setName(l.name)
    setKind(l.kind)
    setBaseUrl(l.base_url ?? '')
    setListId(l.list_id ? String(l.list_id) : '')
    setApiKey('') // masked round-trip: never pre-filled with the stored key
    setProfileIds((l.profile_ids ?? []).join(', '))
    setTagIds((l.tag_ids ?? []).join(', '))
    setRootPaths((l.root_folder_paths ?? []).join(', '))
  }

  function clearDraft() {
    setEditId(null)
    setName('')
    setKind('goodreads')
    setBaseUrl('')
    setListId('')
    setApiKey('')
    setProfileIds('')
    setTagIds('')
    setRootPaths('')
  }

  function submitDraft() {
    const n = name.trim()
    if (!n) return
    const isGoodreads = kind === 'goodreads'
    const url = baseUrl.trim() || (isGoodreads ? GOODREADS_DEFAULT_URL : '')
    if (!url) return
    if (!isGoodreads && !/^https?:\/\//i.test(url)) return
    if (isGoodreads) {
      const idNum = Number(listId) || 0
      if (idNum < 1) return
      // The draft omits id/enabled: the row keeps the existing id, and a
      // new list starts enabled (the shared import-lists convention).
      const base: Omit<BooksBookImportList, 'id' | 'enabled'> = {
        name: n,
        kind,
        base_url: url,
        list_id: idNum,
        api_key: '',
        api_key_set: false,
        profile_ids: [],
        tag_ids: [],
        root_folder_paths: [],
      }
      if (editId !== null) {
        setLists((ls) => ls.map((x) => (x.id === editId ? { ...x, ...base } : x)))
      } else {
        const id = lists.reduce((m, x) => Math.max(m, x.id), 0) + 1
        setLists((ls) => [...ls, { id, enabled: true, ...base }])
      }
      clearDraft()
      return
    }
    const base: Omit<BooksBookImportList, 'id' | 'enabled' | 'api_key_set'> = {
      name: n,
      kind,
      base_url: url,
      list_id: 0,
      api_key: apiKey,
      profile_ids: parseIds(profileIds),
      tag_ids: parseIds(tagIds),
      root_folder_paths: parsePaths(rootPaths),
    }
    if (editId !== null) {
      // Masked key round-trip: a blank key keeps the stored one.
      setLists((ls) =>
        ls.map((x) =>
          x.id === editId ? { ...x, ...base, api_key_set: x.api_key_set || apiKey !== '' } : x,
        ),
      )
    } else {
      const id = lists.reduce((m, x) => Math.max(m, x.id), 0) + 1
      setLists((ls) => [...ls, { id, enabled: true, ...base, api_key_set: apiKey !== '' }])
    }
    clearDraft()
  }

  function addExclusion() {
    const f = excForeign.trim()
    const nm = excName.trim()
    if (!f && !nm) return
    setExclusions((xs) => [...xs, { foreign_id: f, name: nm }])
    setExcForeign('')
    setExcName('')
  }

  function removeExclusion(i: number) {
    setExclusions((xs) => xs.filter((_, idx) => idx !== i))
  }

  function handleSave() {
    void submit(async () => {
      const next: BooksBookImportListsSettings = { lists, exclusions }
      await client.putSettingsSection<BooksBookImportListsSettings>('book-import-lists', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  // The address summary per row: goodreads shows site + list id; the
  // instance kinds show the base URL (+ key-stored note + the readarr
  // source filters).
  function addressOf(l: BooksBookImportList): string {
    if (l.kind === 'goodreads') return `${l.base_url} / list ${l.list_id}`
    const filters =
      l.kind === 'readarr'
        ? [
            l.profile_ids.length ? `profiles [${l.profile_ids.join(', ')}]` : '',
            l.tag_ids.length ? `tags [${l.tag_ids.join(', ')}]` : '',
            l.root_folder_paths.length ? `roots [${l.root_folder_paths.join(', ')}]` : '',
          ]
            .filter(Boolean)
            .join(' ')
        : ''
    return `${l.base_url}${l.api_key_set ? ' (key stored)' : ''}${filters ? ` ${filters}` : ''}`
  }

  return (
    <>
      <SectionCard
        title="Books import lists"
        hint="Goodreads lists, LazyLibrarian instances, and Readarr instances (drop-in bridges). Exclusions apply to every list in this document."
        testId="blb-card"
      >
        {lists.map((l) => (
          <div className="subform" key={l.id} data-testid={`blb-item-${l.id}`}>
            <span className="pill" data-testid={`blb-kind-${l.id}`}>
              {l.kind}
            </span>
            <span data-testid={`blb-name-${l.id}`}>{l.name}</span>
            <label className="check">
              <input
                type="checkbox"
                checked={l.enabled}
                onChange={(e) =>
                  setLists((ls) => ls.map((x) => (x.id === l.id ? { ...x, enabled: e.target.checked } : x)))
                }
                data-testid={`blb-enabled-${l.id}`}
              />
              enabled
            </label>
            <span className="dim small" data-testid={`blb-address-${l.id}`}>
              {addressOf(l)}
            </span>
            <button className="ghost" onClick={() => beginEdit(l)} data-testid={`blb-edit-${l.id}`}>
              Edit
            </button>
            <button
              className="ghost"
              onClick={() => setLists((ls) => ls.filter((x) => x.id !== l.id))}
              data-testid={`blb-del-${l.id}`}
            >
              Delete
            </button>
          </div>
        ))}
        {lists.length === 0 && <p className="dim small">No books import lists configured.</p>}
        {saved && (
          <p className="pill pill-ok" data-testid="blb-saved">
            Saved
          </p>
        )}
        <SaveError error={saveError} testId="blb-error-save" />
        <button onClick={handleSave} disabled={busy || loading} data-testid="blb-save">
          {busy ? 'Saving…' : 'Save books import lists'}
        </button>
      </SectionCard>

      <SectionCard
        title={editId !== null ? `Edit list ${editId}` : 'Add a books import list'}
        testId="blb-add-card"
      >
        <label htmlFor="blb-new-name">Name</label>
        <input id="blb-new-name" value={name} onChange={(e) => setName(e.target.value)} data-testid="blb-new-name" />
        <label htmlFor="blb-new-kind">Source</label>
        <select
          id="blb-new-kind"
          value={kind}
          onChange={(e) => setKind(e.target.value as (typeof BOOK_IMPORT_LIST_KINDS)[number])}
          data-testid="blb-new-kind"
        >
          {BOOK_IMPORT_LIST_KINDS.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        {kind === 'goodreads' ? (
          <>
            <label htmlFor="blb-new-listid">Goodreads list id</label>
            <input
              id="blb-new-listid"
              type="number"
              value={listId}
              onChange={(e) => setListId(e.target.value)}
              data-testid="blb-new-listid"
            />
            <label htmlFor="blb-new-baseurl">Goodreads site (base URL)</label>
            <input
              id="blb-new-baseurl"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder={GOODREADS_DEFAULT_URL}
              data-testid="blb-new-baseurl"
            />
          </>
        ) : (
          <>
            <label htmlFor="blb-new-baseurl">
              {kind === 'lazylibrarian' ? 'LazyLibrarian base URL' : 'Readarr instance base URL'}
            </label>
            <input
              id="blb-new-baseurl"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="http://lazylibrarian.local:5200"
              data-testid="blb-new-baseurl"
            />
            <label htmlFor="blb-new-key">
              {kind === 'lazylibrarian' ? 'LazyLibrarian' : 'Readarr'} API key{' '}
              {editId !== null && lists.find((l) => l.id === editId)?.api_key_set
                ? '(stored — blank to keep)'
                : ''}
            </label>
            <input
              id="blb-new-key"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              data-testid="blb-new-key"
            />
          </>
        )}
        {kind === 'readarr' && (
          <>
            {/* The Readarr bridge's source-side filters: empty = all. */}
            <label htmlFor="blb-new-profiles">Profile ids (comma-separated; empty = all)</label>
            <input
              id="blb-new-profiles"
              value={profileIds}
              onChange={(e) => setProfileIds(e.target.value)}
              placeholder="1, 2"
              data-testid="blb-new-profiles"
            />
            <label htmlFor="blb-new-tags">Tag ids (comma-separated; empty = all)</label>
            <input
              id="blb-new-tags"
              value={tagIds}
              onChange={(e) => setTagIds(e.target.value)}
              placeholder="1, 3"
              data-testid="blb-new-tags"
            />
            <label htmlFor="blb-new-roots">Root folder paths (comma-separated; empty = all)</label>
            <input
              id="blb-new-roots"
              value={rootPaths}
              onChange={(e) => setRootPaths(e.target.value)}
              placeholder="/data/books"
              data-testid="blb-new-roots"
            />
          </>
        )}
        <div>
          <button className="ghost" onClick={submitDraft} data-testid="blb-add">
            {editId !== null ? 'Apply changes' : 'Add list'}
          </button>{' '}
          {editId !== null && (
            <button className="ghost" onClick={clearDraft} data-testid="blb-edit-cancel">
              Cancel
            </button>
          )}
        </div>
      </SectionCard>

      <SectionCard
        title="Exclusions"
        hint="Books no list should ever import — matched by the source's foreign id or the book name."
        testId="blb-exclusions-card"
      >
        <div data-testid="blb-exclusions">
          {exclusions.map((x, i) => (
            <div className="subform" key={i} data-testid={`blb-exc-${i}`}>
              <span data-testid={`blb-exc-foreign-${i}`}>{x.foreign_id}</span>
              <span data-testid={`blb-exc-name-${i}`}>{x.name}</span>
              <button className="ghost" onClick={() => removeExclusion(i)} data-testid={`blb-exc-del-${i}`}>
                Remove
              </button>
            </div>
          ))}
        </div>
        {exclusions.length === 0 && <p className="dim small">No exclusions.</p>}
        <div className="row">
          <input
            placeholder="Foreign id"
            value={excForeign}
            onChange={(e) => setExcForeign(e.target.value)}
            data-testid="blb-exc-foreign"
          />
          <input
            placeholder="Book name"
            value={excName}
            onChange={(e) => setExcName(e.target.value)}
            data-testid="blb-exc-name"
          />
          <button
            onClick={addExclusion}
            disabled={!excForeign.trim() && !excName.trim()}
            data-testid="blb-exc-add"
          >
            Add exclusion
          </button>
        </div>
      </SectionCard>
    </>
  )
}
