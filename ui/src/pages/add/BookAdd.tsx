import { useCallback, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { SearchResult } from '../../api/types'
import {
  AddError,
  MonitoredField,
  ResultList,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

// BookAddPage is the book add flow (#42): search Open Library works → pick a
// work → add. The books model is hierarchical (author → title → edition), so
// the add is two calls: POST /books/authors (idempotent — an existing author
// returns its id), then POST /books/authors/{id}/titles. Both are monitored
// by the same toggle. There is no quality-profile picker: book "quality" is
// an edition attribute (format), not a title attribute.
export function BookAddPage() {
  const navigate = useNavigate()
  const [author, setAuthor] = useState('')
  const search = useCallback(
    (q: string) => client.searchBookWorks(q, author.trim()),
    [author],
  )
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<SearchResult | null>(null)
  const [title, setTitle] = useState('')
  const [monitored, setMonitored] = useState(true)

  // Readarr B1: an optional edition for the new title. An edition is the
  // (format, language, monitored) triple — the books analogue of Radarr's
  // edition model. Format is required for an edition; language is optional.
  const [editionFormat, setEditionFormat] = useState<'' | 'epub' | 'mobi' | 'azw3'>('')
  const [editionLanguage, setEditionLanguage] = useState('')

  const { busy, error, submit } = useAddSubmit(async () => {
    // 1. The author (created if new; idempotent for an existing one).
    const res = await client.addAuthor({ name: author.trim(), monitored })
    // 2. The title within the author.
    const titleRes = await client.addTitle(res.id, { name: title.trim(), monitored })
    // 3. The optional edition (manual entry; Open Library resolution of
    //    editions lands in a later phase).
    if (editionFormat) {
      await client.addEdition(titleRes.id, {
        format: editionFormat,
        language: editionLanguage.trim() || undefined,
        monitored,
      })
    }
    navigate('/books')
  })

  function pick(hit: SearchResult) {
    setSelected(hit)
    setTitle(hit.title)
    if (hit.extra.author) setAuthor(hit.extra.author)
  }

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="book-add-title">Add book</h2>
        <Link className="ghost" to="/books" data-testid="book-add-back">
          ← All books
        </Link>
      </div>

      <SearchBox
        label="Search works (Open Library)"
        placeholder="Title, e.g. The Left Hand of Darkness"
        text={text}
        onText={setText}
        state={state}
        testId="book-search"
      />

      {state.phase === 'results' && (
        <ResultList hits={state.hits} onPick={pick} testId="book-results" />
      )}

      <div className="card add-form" data-testid="book-add-form">
        {selected && (
          <p className="dim small" data-testid="book-selected">
            Selected: <strong>{selected.title}</strong>
            {selected.year ? ` (${selected.year})` : ''}
          </p>
        )}
        <label htmlFor="book-author">Author</label>
        <input
          id="book-author"
          value={author}
          onChange={(e) => setAuthor(e.target.value)}
          placeholder="Author name"
          data-testid="book-author-input"
        />
        <label htmlFor="book-title">Title</label>
        <input
          id="book-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Book title"
          data-testid="book-title-input"
        />
        <MonitoredField
          label="Monitor for new editions"
          checked={monitored}
          onChecked={setMonitored}
          testId="book-monitored"
        />
        <label htmlFor="book-edition-format">Edition format (optional)</label>
        <select
          id="book-edition-format"
          value={editionFormat}
          onChange={(e) => setEditionFormat(e.target.value as '' | 'epub' | 'mobi' | 'azw3')}
          data-testid="book-edition-format"
        >
          <option value="">— no edition —</option>
          <option value="epub">EPUB</option>
          <option value="mobi">MOBI</option>
          <option value="azw3">AZW3</option>
        </select>
        {editionFormat !== '' && (
          <>
            <label htmlFor="book-edition-language">Edition language (optional)</label>
            <input
              id="book-edition-language"
              value={editionLanguage}
              onChange={(e) => setEditionLanguage(e.target.value)}
              placeholder="e.g. en"
              data-testid="book-edition-language"
            />
          </>
        )}
        <AddError message={error} testId="book-add-error" />
        <button
          type="button"
          disabled={busy || !author.trim() || !title.trim()}
          onClick={() => {
            void submit().catch(() => {
              /* error surfaced in AddError */
            })
          }}
          data-testid="book-add-button"
        >
          {busy ? 'Adding…' : 'Add book'}
        </button>
      </div>
    </div>
  )
}
