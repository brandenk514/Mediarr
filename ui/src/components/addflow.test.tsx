import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../api/client'
import type { SearchResult } from '../api/types'
import {
  ProfilePicker,
  ResultList,
  SearchBox,
  useDebouncedSearch,
} from './addflow'

vi.mock('../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  },
  client: {
    getProfiles: vi.fn(),
  },
}))

function hit(id: string, title: string, year = 2020, extra: Record<string, string> = {}): SearchResult {
  return { id, title, year, extra }
}

// A tiny component that exposes useDebouncedSearch's state for assertions.
function Probe({ search }: { search: (q: string) => Promise<SearchResult[]> }) {
  const { text, setText, state } = useDebouncedSearch(search)
  return (
    <div>
      <input
        data-testid="probe-input"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <span data-testid="probe-phase">{state.phase}</span>
      {state.error && <span data-testid="probe-error">{state.error}</span>}
      {state.hits.map((h) => (
        <span key={h.id} data-testid={`probe-hit-${h.id}`}>
          {h.title}
        </span>
      ))}
    </div>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('useDebouncedSearch', () => {
  it('does not search below the minimum length', async () => {
    const search = vi.fn().mockResolvedValue([])
    const user = userEvent.setup()
    render(<Probe search={search} />)
    await user.type(screen.getByTestId('probe-input'), 'a')
    await new Promise((r) => setTimeout(r, 450))
    expect(search).not.toHaveBeenCalled()
  })

  it('debounces the term and surfaces the hits', async () => {
    const search = vi.fn().mockResolvedValue([hit('1', 'Dune: Part Two', 2024)])
    const user = userEvent.setup()
    render(<Probe search={search} />)
    await user.type(screen.getByTestId('probe-input'), 'dune')
    // Debounce elapses (350ms) + search resolves.
    await waitFor(() => expect(screen.getByTestId('probe-phase')).toHaveTextContent('results'))
    expect(search).toHaveBeenCalledWith('dune')
    expect(screen.getByTestId('probe-hit-1')).toBeInTheDocument()
  })

  it('drops a stale response when a newer search wins the race', async () => {
    let first: (hits: SearchResult[]) => void
    const search = vi.fn(
      (q: string) =>
        new Promise<SearchResult[]>((resolve) => {
          if (q === 'du') {
            first = resolve
          } else {
            // Second search resolves immediately with the correct hits.
            resolve([hit('9', 'Dune: Part Two', 2024)])
          }
        }),
    )
    const user = userEvent.setup()
    render(<Probe search={search} />)
    await user.type(screen.getByTestId('probe-input'), 'du')
    await waitFor(() => expect(search).toHaveBeenCalledTimes(1))
    // Second keystroke batch fires a newer search; the first one resolves late
    // with the wrong (stale) hits and must be ignored.
    await user.type(screen.getByTestId('probe-input'), 'ne')
    await waitFor(() => expect(screen.getByTestId('probe-phase')).toHaveTextContent('results'))
    first!([]) // late, stale response
    await waitFor(() => expect(screen.getByTestId('probe-phase')).toHaveTextContent('results'))
    expect(screen.getByTestId('probe-hit-9')).toBeInTheDocument()
  })

  it('greys the box on a 503 (provider not configured) without a retry', async () => {
    const search = vi
      .fn()
      .mockRejectedValue(new ApiError(503, 'tmdb is not configured (missing API key)'))
    const user = userEvent.setup()
    render(<Probe search={search} />)
    await user.type(screen.getByTestId('probe-input'), 'dune')
    await waitFor(() => expect(screen.getByTestId('probe-phase')).toHaveTextContent('unavailable'))
    expect(screen.getByTestId('probe-error')).toHaveTextContent('not configured')
  })

  it('surfaces other failures as a retry-able error', async () => {
    const search = vi.fn().mockRejectedValue(new Error('tmdb search failed: 500'))
    const user = userEvent.setup()
    render(<Probe search={search} />)
    await user.type(screen.getByTestId('probe-input'), 'dune')
    await waitFor(() => expect(screen.getByTestId('probe-phase')).toHaveTextContent('error'))
    expect(screen.getByTestId('probe-error')).toHaveTextContent('search failed')
  })
})

describe('SearchBox', () => {
  it('disables the input and shows the reason when the provider is unavailable', () => {
    render(
      <SearchBox
        label="Search movies (TMDB)"
        placeholder="p"
        text=""
        onText={() => {}}
        state={{ phase: 'unavailable', hits: [], error: 'tmdb is not configured (missing API key)' }}
        testId="movie-search"
      />,
    )
    expect(screen.getByTestId('movie-search-input')).toBeDisabled()
    expect(screen.getByTestId('movie-search-unavailable')).toHaveTextContent('not configured')
  })
})

describe('ResultList', () => {
  it('renders hits as selectable cards with detail and selects on click', async () => {
    const onPick = vi.fn()
    const user = userEvent.setup()
    render(
      <ResultList
        hits={[hit('1', 'Dune: Part Two', 2024, { overview: 'Fremen.', rating: '7.8' })]}
        onPick={onPick}
        testId="movie-results"
      />,
    )
    await user.click(screen.getByTestId('result-1'))
    expect(onPick).toHaveBeenCalledWith(
      expect.objectContaining({ id: '1', title: 'Dune: Part Two' }),
    )
  })

  it('shows an empty note when there are no hits', () => {
    render(<ResultList hits={[]} onPick={() => {}} testId="movie-results" />)
    expect(screen.getByTestId('movie-results-empty')).toBeInTheDocument()
  })
})

describe('ProfilePicker', () => {
  it('prefills the kind default from the API and switches on change', async () => {
    vi.mocked(client.getProfiles).mockResolvedValue({
      profiles: ['SD', 'HD-1080p', 'UHD-2160p'],
      default: 'HD-1080p',
    })
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<ProfilePicker kind="movies" value="" onChange={onChange} testId="movie-profile" />)

    // The default is pushed to the page once the list arrives.
    await waitFor(() => expect(onChange).toHaveBeenCalledWith('HD-1080p'))

    await user.selectOptions(screen.getByTestId('movie-profile'), 'UHD-2160p')
    expect(onChange).toHaveBeenCalledWith('UHD-2160p')
  })

  it('surfaces a profile-load failure inline', async () => {
    vi.mocked(client.getProfiles).mockRejectedValue(new Error('boom'))
    render(<ProfilePicker kind="tv" value="" onChange={() => {}} testId="tv-profile" />)
    expect(await screen.findByTestId('tv-profile-error')).toHaveTextContent('boom')
  })
})
