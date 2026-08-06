import { afterEach, describe, expect, it, vi } from 'vitest'
import { deviceFarmFetch } from './fetcher'

describe('deviceFarmFetch', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('adds an idempotency key to device mutation requests', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ request_id: 'req_test', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await deviceFarmFetch('/api/v1/devices/device-1/restarts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason: 'test' }),
    })

    const request = fetchMock.mock.calls[0]
    const headers = new Headers(request[1]?.headers)
    expect(headers.get('Idempotency-Key')).toMatch(/^console-.{8,}$/)
  })
})
