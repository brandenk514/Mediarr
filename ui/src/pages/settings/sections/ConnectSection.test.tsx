import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../../api/client'
import { ConnectSection } from './ConnectSection'

// Mock the typed client; the feed rides the per-section document endpoint,
// and the Test button rides the P5 /connect/test endpoint.
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
    testConnectNotification: vi.fn(),
  },
}))

const FRESH = {
  notifications: [
    {
      id: 1,
      name: 'Plex home',
      kind: 'plex',
      enabled: true,
      options: { url: 'http://plex:32400', section_id: '1', token: '', token_set: true },
    },
  ],
}

function renderSection() {
  return render(
    <MemoryRouter>
      <ConnectSection />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(client.getSettingsSection as ReturnType<typeof vi.fn>).mockResolvedValue(FRESH)
  ;(client.putSettingsSection as ReturnType<typeof vi.fn>).mockImplementation(
    async (_s: string, doc: unknown) => doc as never,
  )
  ;(client.testConnectNotification as ReturnType<typeof vi.fn>).mockResolvedValue({
    ok: true,
    message: 'delivered to test sink',
  })
})

describe('ConnectSection', () => {
  it('renders the seeded notification (masked token)', async () => {
    renderSection()
    expect(await screen.findByTestId('connect-item-1')).toBeInTheDocument()
    expect(screen.getByTestId('connect-kind-1')).toHaveTextContent('plex')
    expect(screen.getByTestId('connect-url-1')).toHaveTextContent('http://plex:32400 (token stored)')
  })

  it('fires a test event through the /connect/test endpoint', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('connect-item-1')
    await user.click(screen.getByTestId('connect-test-1'))
    await waitFor(() =>
      expect(client.testConnectNotification).toHaveBeenCalledWith({
        id: 1,
        kind: 'plex',
        options: { url: 'http://plex:32400', section_id: '1', token: '', token_set: true },
      }),
    )
    expect(await screen.findByTestId('connect-test-result-1')).toHaveTextContent('ok: delivered to test sink')
  })

  it('adds a webhook notification and saves the feed', async () => {
    const user = userEvent.setup()
    renderSection()
    await screen.findByTestId('connect-item-1')
    await user.type(screen.getByTestId('connect-new-name'), 'hooks')
    await user.selectOptions(screen.getByTestId('connect-new-kind'), 'webhook')
    await user.type(screen.getByTestId('connect-new-url'), 'https://h.example.com/n')
    await user.click(screen.getByTestId('connect-add'))
    expect(await screen.findByTestId('connect-item-2')).toBeInTheDocument()
    await user.click(screen.getByTestId('connect-save'))
    await waitFor(() =>
      expect(client.putSettingsSection).toHaveBeenCalledWith('connect', {
        notifications: [
          {
            id: 1,
            name: 'Plex home',
            kind: 'plex',
            enabled: true,
            options: { url: 'http://plex:32400', section_id: '1', token: '', token_set: true },
          },
          {
            id: 2,
            name: 'hooks',
            kind: 'webhook',
            enabled: true,
            options: { url: 'https://h.example.com/n', section_id: '', token: '', token_set: false },
          },
        ],
      }),
    )
  })
})
