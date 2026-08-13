import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_REQUEST_TIMEOUT_MS, DeviceFarmAPIError, deviceFarmFetch } from './fetcher'

describe('deviceFarmFetch', () => {
  afterEach(() => {
    vi.useRealTimers()
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

  it('aborts a request that exceeds the explicit client timeout', async () => {
    vi.useFakeTimers()
    vi.spyOn(globalThis, 'fetch').mockImplementation((_input, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
    }))

    const request = deviceFarmFetch('/console/api/v1/devices/device-1/remote-control', { method: 'POST' })
    const rejection = expect(request).rejects.toMatchObject({
      code: 'REQUEST_TIMEOUT',
      retryable: true,
      status: 0,
    } satisfies Partial<DeviceFarmAPIError>)
    await vi.advanceTimersByTimeAsync(DEFAULT_REQUEST_TIMEOUT_MS)
    await rejection
  })

  it('routes an embedded console request through the Alcor gateway', async () => {
    const topDescriptor = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ request_id: 'req_embedded', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    try {
      await deviceFarmFetch('/console/api/v1/devices/device-1/remote-control', { method: 'POST' })
      const [url, init] = fetchMock.mock.calls[0]
      expect(url).toBe('/api/v2/device-farm/proxy/api/v1/devices/device-1/remote-control')
      expect(new Headers(init?.headers).get('X-Alcor-Device-Farm')).toBe('embedded-console')
    } finally {
      if (topDescriptor) Object.defineProperty(window, 'top', topDescriptor)
    }
  })
})
