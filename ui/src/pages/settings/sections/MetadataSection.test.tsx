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
  // Lidarr L3 music block (server defaults: tag on, no scrub, consumers off).
  tag_audio_files: true,
  scrub_existing_tags: false,
  consumers: [
    { name: 'Kodi (XBMC)/Emby', enabled: false },
    { name: 'Roksbox', enabled: false },
    { name: 'WDTV', enabled: false },
  ],
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
        // Lidarr L3 music fields round-trip unchanged (the doc's values).
        tag_audio_files: true,
        scrub_existing_tags: false,
        consumers: [
          { name: 'Kodi (XBMC)/Emby', enabled: false },
          { name: 'Roksbox', enabled: false },
          { name: 'WDTV', enabled: false },
        ],
      }),
    )
  })

  it('renders the L3 music block and saves consumer + scrub edits', async () => {
    const user = userEvent.setup()
    renderSection()
    // Seeded state: tag on, scrub off, the 3 consumers all disabled. The
    // consumer rows only exist once the document load has seeded them.
    expect(await screen.findByTestId('md-tag-audio')).toBeChecked()
    expect(screen.getByTestId('md-scrub')).not.toBeChecked()
    await screen.findByTestId('md-consumer-Roksbox')
    for (const name of ['Kodi (XBMC)/Emby', 'Roksbox', 'WDTV']) {
      expect(screen.getByTestId(`md-consumer-${name}`)).not.toBeChecked()
    }

    await user.click(screen.getByTestId('md-scrub'))
    await user.click(screen.getByTestId('md-consumer-Roksbox'))
    await user.click(screen.getByTestId('metadata-save'))
    await vi.waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'metadata',
        expect.objectContaining({
          tag_audio_files: true,
          scrub_existing_tags: true,
          consumers: [
            { name: 'Kodi (XBMC)/Emby', enabled: false },
            { name: 'Roksbox', enabled: true },
            { name: 'WDTV', enabled: false },
          ],
        }),
      ),
    )
  })
})
