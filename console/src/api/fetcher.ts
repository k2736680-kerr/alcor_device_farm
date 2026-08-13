export class DeviceFarmAPIError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly requestId: string,
    readonly retryable: boolean,
    readonly status: number,
  ) {
    super(message)
  }
}

type ErrorEnvelope = {
  request_id?: string
  error?: { code?: string; message?: string; retryable?: boolean } | null
}

export const DEFAULT_REQUEST_TIMEOUT_MS = 15_000

function embeddedURL(url: string): string {
  if (window.self === window.top) return url
  if (url.startsWith('/api/v1/')) return `/api/v2/device-farm/proxy${url}`
  if (url === '/console/api/v1/me') return '/api/v2/device-farm/session'
  if (url.startsWith('/console/api/v1/devices/')) {
    return `/api/v2/device-farm/proxy${url.replace('/console/api/v1', '/api/v1')}`
  }
  return url
}

function cookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`
  return document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(prefix))?.slice(prefix.length)
}

export async function deviceFarmFetch<T>(url: string, options: RequestInit): Promise<T> {
  const headers = new Headers(options.headers)
  const method = (options.method ?? 'GET').toUpperCase()
  if (window.self !== window.top) headers.set('X-Alcor-Device-Farm', 'embedded-console')
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    if (!headers.has('Idempotency-Key')) {
      const requestKey = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
      headers.set('Idempotency-Key', `console-${requestKey}`)
    }
    const csrf = cookie('device_farm_csrf')
    if (csrf) headers.set('X-CSRF-Token', decodeURIComponent(csrf))
  }

  const controller = new AbortController()
  const upstreamSignal = options.signal
  let timedOut = false
  const forwardAbort = () => controller.abort(upstreamSignal?.reason)
  if (upstreamSignal?.aborted) {
    forwardAbort()
  } else {
    upstreamSignal?.addEventListener('abort', forwardAbort, { once: true })
  }
  const timeout = globalThis.setTimeout(() => {
    timedOut = true
    controller.abort()
  }, DEFAULT_REQUEST_TIMEOUT_MS)

  try {
    const response = await fetch(embeddedURL(url), { ...options, headers, signal: controller.signal, credentials: 'same-origin' })
    const payload = (await response.json()) as T & ErrorEnvelope
    if (!response.ok) {
      throw new DeviceFarmAPIError(
        payload.error?.message ?? `请求失败 (${response.status})`,
        payload.error?.code ?? 'HTTP_ERROR',
        payload.request_id ?? response.headers.get('X-Request-Id') ?? '-',
        payload.error?.retryable ?? false,
        response.status,
      )
    }
    return payload
  } catch (error) {
    if (error instanceof DeviceFarmAPIError) {
      throw error
    }
    if (timedOut) {
      throw new DeviceFarmAPIError('请求超时，请检查服务状态后重试', 'REQUEST_TIMEOUT', '-', true, 0)
    }
    if (upstreamSignal?.aborted) {
      throw error
    }
    throw new DeviceFarmAPIError('无法连接设备农场服务，请检查网络后重试', 'NETWORK_ERROR', '-', true, 0)
  } finally {
    globalThis.clearTimeout(timeout)
    upstreamSignal?.removeEventListener('abort', forwardAbort)
  }
}
