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

// 曾经在这里把控制台 API 翻译成 `/api/v2/device-farm/proxy/...` 供 Alcor 后端
// 代理到 Device Farm。但 Alcor 后端并未提供该代理路由（仅 GET 路径偶尔工作，
// POST 会被静默吞掉），导致控制台在 iframe 嵌入模式下任何写操作都返回
// `HTTP_ERROR / request_id=-` 的假死错误。
//
// 控制台构建在 Device Farm server 内置的静态服务上（gateway 或 server 自身），
// 浏览器访问控制台时 origin 就是 Device Farm 自身的 origin，直接打 `/api/v1/...`
// 就等于打 Device Farm server，完全不需要任何代理前缀。
//
// 如果未来 Alcor 确实要把控制台嵌入到它自己的 origin 下，那应该由 Alcor 后端
// 提供同源代理，而不是 Device Farm 前端猜 Alcor 的 URL 结构。
function embeddedURL(url: string): string {
  return url
}

function cookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`
  return document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(prefix))?.slice(prefix.length)
}

export async function deviceFarmFetch<T>(url: string, options: RequestInit): Promise<T> {
  const headers = new Headers(options.headers)
  const method = (options.method ?? 'GET').toUpperCase()
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
