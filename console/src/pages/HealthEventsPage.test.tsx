import { screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { sampleHealthEvents } from '../test/handlers'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { HealthEventsPage } from './HealthEventsPage'

describe('HealthEventsPage device selection', () => {
  it('selects a faulted device first so the operator sees actionable events', async () => {
    let requestedDeviceID = ''
    server.use(http.get('/api/v1/devices/:id/health-events', ({ params }) => {
      requestedDeviceID = String(params.id)
      const events = sampleHealthEvents.map((event) => ({ ...event, device_id: requestedDeviceID }))
      return HttpResponse.json({
        request_id: 'req_faulted_device_health',
        data: { items: events, total: events.length, page: 1, page_size: 20 },
        error: null,
      })
    }))

    renderWithProviders(<HealthEventsPage />)

    await waitFor(() => expect(requestedDeviceID).toBe('device_00000000000003'))
    expect(await screen.findByText(/emulator-5558/)).toBeInTheDocument()
  })
})
