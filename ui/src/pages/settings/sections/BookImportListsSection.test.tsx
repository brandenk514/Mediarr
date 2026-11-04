import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { BookImportListsSection } from './BookImportListsSection'

// Mock the typed client; the books list configs ride the per-section document
// endpoint (book-import-lists). There is no books run endpoint yet.
vi.mock('../../../api/client', () => ({
  ApiError: class extends Error {
    status: number
    constructor(status: number) {
      super('boom')
      this.status = status
    }
  },
  client: {
    getSettingsSection: vi.fn(),
    putSettingsSection: vi.fn(),
  },
}))

const FRESH = {
  lists: [
    {
      id: 1,
      name: 'best of 2026',
      kind: 'goodreads',
      enabled: true,
      base_url: 'https://www.goodreads.com',
      list_id: 12345,
      api_key: '',
      api_key_set: false,
      profile_ids: [],
      tag_ids: [],
      root_folder_paths: [],
    },
  ],
  exclusions: [],
}

function renderSection() {
  return render(
    <MemoryRouter>
      <BookImportListsSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue(FRESH)
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_s: string, doc: unknown) => doc as never,
  )
})

describe('BookImportListsSection', () => {
  it('renders the seeded goodreads list', async () => {
    renderSection()
    expect(await screen.findByTestId('blb-item-1')).toBeInTheDocument()
    expect(screen.getByTestId('blb-kind-1')).toHaveTextContent('goodreads')
    expect(screen.getByTestId('blb-name-1')).toHaveTextContent('best of 2026')
    expect(screen.getByTestId('blb-enabled-1')).toBeChecked()
    expect(screen.getByTestId('blb-address-1')).toHaveTextContent('https://www.goodreads.com / list 12345')
  })

  it('adds a goodreads list and saves the doc', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('blb-item-1')
    await user.type(screen.getByTestId('blb-new-name'), 'staff picks')
    await user.selectOptions(screen.getByTestId('blb-new-kind'), 'goodreads')
    await user.type(screen.getByTestId('blb-new-listid'), '99')
    await user.type(screen.getByTestId('blb-new-baseurl'), 'https://www.goodreads.com')
    await user.click(screen.getByTestId('blb-add'))
    expect(await screen.findByTestId('blb-item-2')).toBeInTheDocument()
    // The goodreads row shows the list id in the address summary.
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('list 99')
    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({
          lists: [
            expect.objectContaining({ id: 1, kind: 'goodreads' }),
            expect.objectContaining({
              id: 2,
              name: 'staff picks',
              kind: 'goodreads',
              base_url: 'https://www.goodreads.com',
              list_id: 99,
              enabled: true,
            }),
          ],
        }),
      ),
    )
  })

  it('adds a LazyLibrarian list with a (masked) API key and saves the doc', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('blb-item-1')
    await user.type(screen.getByTestId('blb-new-name'), 'll-home')
    await user.selectOptions(screen.getByTestId('blb-new-kind'), 'lazylibrarian')
    // The lazylibrarian form swaps the list-id field for base URL + key.
    expect(screen.queryByTestId('blb-new-listid')).not.toBeInTheDocument()
    await user.type(screen.getByTestId('blb-new-baseurl'), 'http://ll.home.lan:5200')
    await user.type(screen.getByTestId('blb-new-key'), 'sekrit')
    await user.click(screen.getByTestId('blb-add'))
    expect(await screen.findByTestId('blb-item-2')).toBeInTheDocument()
    expect(screen.getByTestId('blb-kind-2')).toHaveTextContent('lazylibrarian')
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('http://ll.home.lan:5200 (key stored)')

    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({
          lists: [
            expect.objectContaining({ id: 1, kind: 'goodreads' }),
            expect.objectContaining({
              id: 2,
              name: 'll-home',
              kind: 'lazylibrarian',
              base_url: 'http://ll.home.lan:5200',
              list_id: 0,
              enabled: true,
              api_key: 'sekrit',
              api_key_set: true,
            }),
          ],
        }),
      ),
    )
  })

  it('adds a Readarr bridge list with source filters and saves the doc', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('blb-item-1')
    await user.type(screen.getByTestId('blb-new-name'), 'readarr-home')
    await user.selectOptions(screen.getByTestId('blb-new-kind'), 'readarr')
    await user.type(screen.getByTestId('blb-new-baseurl'), 'http://readarr.home.lan:8787')
    await user.type(screen.getByTestId('blb-new-key'), 'bridge-key')
    await user.type(screen.getByTestId('blb-new-profiles'), '1, 2')
    await user.type(screen.getByTestId('blb-new-tags'), '3')
    await user.type(screen.getByTestId('blb-new-roots'), '/data/books, /data/abooks')
    await user.click(screen.getByTestId('blb-add'))
    expect(await screen.findByTestId('blb-item-2')).toBeInTheDocument()
    // The row shows the filters alongside the base URL.
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('http://readarr.home.lan:8787 (key stored)')
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('profiles [1, 2]')
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('tags [3]')
    expect(screen.getByTestId('blb-address-2')).toHaveTextContent('roots [/data/books, /data/abooks]')

    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({
          lists: [
            expect.objectContaining({ id: 1, kind: 'goodreads' }),
            expect.objectContaining({
              id: 2,
              name: 'readarr-home',
              kind: 'readarr',
              base_url: 'http://readarr.home.lan:8787',
              list_id: 0,
              enabled: true,
              api_key: 'bridge-key',
              api_key_set: true,
              profile_ids: [1, 2],
              tag_ids: [3],
              root_folder_paths: ['/data/books', '/data/abooks'],
            }),
          ],
        }),
      ),
    )
  })

  it('edits a list with a blank key, keeping the stored secret', async () => {
    const user = userEvent.setup()
    // A seeded readarr list with a stored (masked) key.
    vi.mocked(client.getSettingsSection).mockResolvedValue({
      lists: [
        {
          id: 1,
          name: 'readarr-home',
          kind: 'readarr',
          enabled: true,
          base_url: 'http://readarr.home.lan:8787',
          list_id: 0,
          api_key: '',
          api_key_set: true,
          profile_ids: [1],
          tag_ids: [],
          root_folder_paths: [],
        },
      ],
      exclusions: [],
    })
    renderSection()
    await screen.findByTestId('blb-item-1')
    await user.click(screen.getByTestId('blb-edit-1'))
    // The draft is pre-filled; the key field stays blank and the hint says
    // the stored key is kept.
    expect(screen.getByTestId('blb-new-baseurl')).toHaveValue('http://readarr.home.lan:8787')
    expect(screen.getByTestId('blb-new-key')).toHaveValue('')
    expect(screen.getByTestId('blb-add-card')).toHaveTextContent('(stored — blank to keep)')

    // Change the base URL only, leave the key blank, and save.
    await user.clear(screen.getByTestId('blb-new-baseurl'))
    await user.type(screen.getByTestId('blb-new-baseurl'), 'http://new-readarr.lan:8787')
    await user.click(screen.getByTestId('blb-add'))
    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('book-import-lists', {
        lists: [
          {
            id: 1,
            name: 'readarr-home',
            kind: 'readarr',
            enabled: true,
            base_url: 'http://new-readarr.lan:8787',
            list_id: 0,
            api_key: '',
            api_key_set: true,
            profile_ids: [1],
            tag_ids: [],
            root_folder_paths: [],
          },
        ],
        exclusions: [],
      }),
    )
  })

  it('toggles a list enabled and saves', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('blb-item-1')
    await user.click(screen.getByTestId('blb-enabled-1'))
    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({
          lists: [expect.objectContaining({ id: 1, enabled: false })],
        }),
      ),
    )
  })

  it('adds and removes document-level exclusions', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('blb-item-1')
    // Add a foreign-id + name exclusion.
    await user.type(screen.getByTestId('blb-exc-foreign'), '90210')
    await user.type(screen.getByTestId('blb-exc-name'), 'The Beak Collection')
    await user.click(screen.getByTestId('blb-exc-add'))
    expect(await screen.findByTestId('blb-exc-0')).toBeInTheDocument()
    expect(screen.getByTestId('blb-exc-foreign-0')).toHaveTextContent('90210')
    expect(screen.getByTestId('blb-exc-name-0')).toHaveTextContent('The Beak Collection')

    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({
          exclusions: [{ foreign_id: '90210', name: 'The Beak Collection' }],
        }),
      ),
    )

    // Remove it and save the empty set.
    await user.click(screen.getByTestId('blb-exc-del-0'))
    await user.click(screen.getByTestId('blb-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'book-import-lists',
        expect.objectContaining({ exclusions: [] }),
      ),
    )
  })
})
