import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_REQUEST_TIMEOUT_MS, DeviceFarmAPIError, deviceFarmFetch, fetchAlcorEmbeddedSession } from './fetcher'

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

    const request = deviceFarmFetch('/api/v1/devices/device-1/remote-control', { method: 'POST' })
    const rejection = expect(request).rejects.toMatchObject({
      code: 'REQUEST_TIMEOUT',
      retryable: true,
      status: 0,
    } satisfies Partial<DeviceFarmAPIError>)
    await vi.advanceTimersByTimeAsync(DEFAULT_REQUEST_TIMEOUT_MS)
    await rejection
  })

  it('translates Device Farm API paths when embedded in the Alcor iframe', async () => {
    // Alcor 在 8880 上以同源 iframe 嵌入控制台，并只暴露两个受控代理前缀：
    //   北向 API       → /api/v2/device-farm/proxy/api/v1/*
    //   控制台自身 API → /api/v2/device-farm/console/api/v1/*
    // 若不翻译，8880 上 /api/v1/* 返回 404、/console/api/v1/* 被 SPA fallback
    // 当成静态资源返回 text/html，控制台会误判为未登录并弹出登录页。
    // ⚠️ 但 console 前缀在 Alcor 侧**只注册了 GET**（静态资源处理器），
    // 所以任何非 GET 的控制台调用都不能落在该前缀上，必须改用北向路径 ——
    // 见下面 `routes embedded remote-control calls through the northbound ...`。
    const topDescriptor = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      new Response(JSON.stringify({ request_id: 'req_embedded', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    try {
      await deviceFarmFetch('/api/v1/devices/device-1/runtime-profile-updates', { method: 'POST' })
      expect(fetchMock.mock.calls[0][0]).toBe('/api/v2/device-farm/proxy/api/v1/devices/device-1/runtime-profile-updates')

      await deviceFarmFetch('/console/api/v1/session', { method: 'GET' })
      expect(fetchMock.mock.calls[1][0]).toBe('/api/v2/device-farm/console/api/v1/session')
    } finally {
      if (topDescriptor) Object.defineProperty(window, 'top', topDescriptor)
    }
  })

  it('routes embedded remote-control calls through the northbound Alcor proxy', async () => {
    // 回归用例：远控曾用并行的 Console 家族 `/console/api/v1/devices/{id}/remote-control*`。
    // Alcor 只在 `/api/v2/device-farm/console/*` 上注册了 GET（静态资源前缀），
    // 所以嵌入态的 POST/DELETE 命中 Alcor 的 404（text/plain），
    // `response.json()` 抛错后被兜底成 NETWORK_ERROR，
    // 界面显示「无法连接设备农场服务，请检查网络后重试」。
    // 远控因此必须走北向 `/api/v1/*`，由 Alcor 的 proxy 前缀转发（全方法白名单）。
    const topDescriptor = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      new Response(JSON.stringify({ request_id: 'req_remote', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    try {
      await deviceFarmFetch('/api/v1/devices/device-1/remote-control', { method: 'POST' })
      await deviceFarmFetch('/api/v1/devices/device-1/remote-control/heartbeat', { method: 'POST' })
      await deviceFarmFetch('/api/v1/devices/device-1/remote-control', { method: 'DELETE' })

      expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
        '/api/v2/device-farm/proxy/api/v1/devices/device-1/remote-control',
        '/api/v2/device-farm/proxy/api/v1/devices/device-1/remote-control/heartbeat',
        '/api/v2/device-farm/proxy/api/v1/devices/device-1/remote-control',
      ])
      for (const call of fetchMock.mock.calls) {
        expect(String(call[0])).not.toContain('/device-farm/console/')
      }
    } finally {
      if (topDescriptor) Object.defineProperty(window, 'top', topDescriptor)
    }
  })

  it('keeps the original URL when not embedded and outside the Alcor proxy path', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ request_id: 'req_direct', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await deviceFarmFetch('/api/v1/device-pools', { method: 'GET' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/device-pools')
  })

  it('translates when the Alcor proxy marker is present in the current pathname', async () => {
    const url = new URL(window.location.href)
    url.pathname = '/api/v2/device-farm/console/devices'
    window.history.replaceState({}, '', url)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ request_id: 'req_marker', data: {}, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    try {
      await deviceFarmFetch('/api/v1/device-pools', { method: 'GET' })
      expect(fetchMock.mock.calls[0][0]).toBe('/api/v2/device-farm/proxy/api/v1/device-pools')
    } finally {
      window.history.replaceState({}, '', '/')
    }
  })

  it('does not call the Alcor session endpoint when opened directly', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ request_id: 'req', data: null, error: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    const session = await fetchAlcorEmbeddedSession()

    expect(session).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reads the embedded session from the Alcor proxy when inside the iframe', async () => {
    const topDescriptor = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          data: { user: { id: 'u-1', display_name: '张三', role: 'admin' }, expires_at: '2099-12-31T23:59:59Z' },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    try {
      const session = await fetchAlcorEmbeddedSession()
      expect(fetchMock.mock.calls[0][0]).toBe('/api/v2/device-farm/session')
      expect(session?.user.role).toBe('admin')
      expect(session?.expires_at).toBe('2099-12-31T23:59:59Z')
    } finally {
      if (topDescriptor) Object.defineProperty(window, 'top', topDescriptor)
    }
  })

  it('returns null instead of throwing when the embedded session is rejected', async () => {
    const topDescriptor = Object.getOwnPropertyDescriptor(window, 'top')
    Object.defineProperty(window, 'top', { configurable: true, value: {} })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: { code: 'UNAUTHORIZED' } }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    try {
      await expect(fetchAlcorEmbeddedSession()).resolves.toBeNull()
    } finally {
      if (topDescriptor) Object.defineProperty(window, 'top', topDescriptor)
    }
  })
})
