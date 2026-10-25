import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { MetadataSection } from './MetadataSection'

// Mock the typed client; the section is read-once + save-on-demand over the
// per-section document endpoint.
vi.mock('../../../api/client', () => ({
  client: {
    getSettingsSection: vi.fn(),
    putSettingsSection: vi.fn(),
  },
}))

const FRESH = {
  generate_nfo: true,
  generate_subtitles: false,
  language: 'en',
  kinds: { movie: true, tv: true, music: true, book: true },
}

function renderSection() {
  return render(
    <MemoryRouter>
      <MetadataSection />
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

describe('MetadataSection', () => {
  it('renders the metadata file writer options', async () => {
    renderSection()
    const nfo = await screen.findByTestId('md-nfo')
    expect(nfo).toBeChecked()
    expect(screen.getByTestId('md-subs')).not.toBeChecked()
    expect(screen.getByTestId('md-lang')).toHaveValue('en')
    for (const k of ['movie', 'tv', 'music', 'book']) {
      expect(screen.getByTestId(`md-kind-${k}`)).toBeChecked()
    }
  })

  it('saves toggles + language + kind set', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('md-nfo')
    await user.click(screen.getByTestId('md-subs'))
    await user.clear(screen.getByTestId('md-lang'))
    await user.type(screen.getByTestId('md-lang'), 'fr')
    await user.click(screen.getByTestId('md-kind-music'))
    await user.click(screen.getByTestId('metadata-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('metadata', {
        generate_nfo: true,
        generate_subtitles: true,
        language: 'fr',
        kinds: { movie: true, tv: true, music: false, book: true },
      }),
    )
  })
})
