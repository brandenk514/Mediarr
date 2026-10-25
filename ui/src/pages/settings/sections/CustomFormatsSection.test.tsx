import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { CustomFormatsSection } from './CustomFormatsSection'

// Mock the typed client; the section is read-once over the per-section
// document endpoint, and the score tester rides the P5 score endpoint.
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
    scoreCustomFormats: vi.fn(),
  },
}))

const FRESH = {
  formats: [
    {
      id: 1,
      name: '1080p boost',
      enabled: true,
      score: 25,
      rules: [{ field: 'quality', operator: 'contains', value: '1080p' }],
    },
  ],
}

function renderSection() {
  return render(
    <MemoryRouter>
      <CustomFormatsSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue(FRESH)
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_s: string, doc: unknown) => doc as never,
  )
  ;(client.scoreCustomFormats as ReturnType<typeof vi.fn>).mockResolvedValue({
    total: 25,
    matches: [{ name: '1080p boost', score: 25 }],
  })
})

describe('CustomFormatsSection', () => {
  it('renders the seeded format and its rule', async () => {
    renderSection()
    expect(await screen.findByTestId('cf-item-1')).toBeInTheDocument()
    expect(screen.getByTestId('cf-name-1')).toHaveValue('1080p boost')
    expect(screen.getByTestId('cf-rules-1')).toHaveTextContent('quality contains 1080p')
  })

  it('saves an edited score', async () => {
    const user = userEvent.setup()
    renderSection()
    const score = await screen.findByTestId('cf-score-1')
    await waitFor(() => expect(screen.getByTestId('cf-save')).toBeEnabled())
    fireEvent.change(score, { target: { value: '30' } })
    await user.click(screen.getByTestId('cf-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('custom-formats', {
        formats: [
          {
            id: 1,
            name: '1080p boost',
            enabled: true,
            score: 30,
            rules: [{ field: 'quality', operator: 'contains', value: '1080p' }],
          },
        ],
      }),
    )
  })

  it('adds a format with a rule and saves it', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('cf-item-1')
    await user.type(screen.getByTestId('cf-new-name'), 'web only')
    await user.type(screen.getByTestId('cf-rule-value'), 'WEB')
    await user.click(screen.getByTestId('cf-add-rule'))
    await user.click(screen.getByTestId('cf-add'))
    // The new format (id 2) renders in the list.
    expect(await screen.findByTestId('cf-item-2')).toBeInTheDocument()
    await user.click(screen.getByTestId('cf-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith(
        'custom-formats',
        expect.objectContaining({
          formats: [
            expect.objectContaining({ id: 1 }),
            expect.objectContaining({
              id: 2,
              name: 'web only',
              enabled: true,
              rules: [{ field: 'title', operator: 'contains', value: 'WEB' }],
            }),
          ],
        }),
      ),
    )
  })

  it('runs the score tester through the P5 score endpoint', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('cf-item-1')
    await user.type(screen.getByTestId('cf-sc-title'), 'Show.S01E01.1080p.WEB')
    await user.click(screen.getByTestId('cf-score'))
    await waitFor(() =>
      expect(client.scoreCustomFormats).toHaveBeenCalledWith({
        title: 'Show.S01E01.1080p.WEB',
        quality: '',
        source: '',
        kind: '',
        size_mb: 0,
        tags: [],
      }),
    )
    expect(await screen.findByTestId('cf-score-total')).toHaveTextContent('total: 25')
    expect(screen.getByTestId('cf-match-1080p boost')).toHaveTextContent('1080p boost: 25')
  })
})
