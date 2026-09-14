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

// Alcor 以同源受控代理方式嵌入本控制台（ADR-0019）。浏览器访问控制台时
// 有两种 origin，需要翻译成对应的后端路径：
//
//   1) 直接访问 Device Farm（origin = Device Farm 自身端口）：
//      控制台 bundle 直连 Device Farm 的 API 即可，`/api/v1/*` 和
//      `/console/api/v1/*` 都不需要翻译。
//
//   2) 在 Alcor iframe 内（origin = Alcor 后端 8880）：
//      控制台必须走 Alcor 的同源代理，否则 8880 上没有 `/api/v1/*` 和
//      `/console/api/v1/*` 路由（前者返回 404，后者被 SPA fallback 当成
//      静态资源返回 text/html），控制台会因解析失败误判为未登录并弹出
//      设备农场自己的登录页。
//      Alcor 代理路径：
//        北向 API       → /api/v2/device-farm/proxy/api/v1/*
//        控制台自身 API → /api/v2/device-farm/console/api/v1/*
//
// 判定信号：
//   a) location.pathname 含 `/api/v2/device-farm/console/` —— Alcor 后端
//      代理 HTML 时把原 `/console/` 前缀替换出来的，是 Alcor 嵌入的**专属**
//      特征，直接访问控制台时不会出现。这是首选依据。
//   b) `window.self !== window.top` —— 真的处于 iframe 中。仅作为 (a) 失效
//      时的兜底（SPA 路由切换或 URL 被规范化后 pathname 可能丢前缀）。
// 注意 (b) 不能单独作为首选：任何人把控制台手动嵌进自己的页面都会命中，
// 从而把请求发往并不存在的 Alcor 代理前缀。
const ALCOR_EMBEDDED_MARKER = '/api/v2/device-farm/console/'

function hasAlcorProxyMarker(): boolean {
  try {
    return window.location?.pathname?.includes(ALCOR_EMBEDDED_MARKER) === true
  } catch {
    return false
  }
}

function isInsideFrame(): boolean {
  try {
    return window.self !== window.top
  } catch {
    // 跨域访问 window.top 会抛异常 —— 这本身就说明我们被嵌在别处。
    return true
  }
}

function isEmbeddedInAlcor(): boolean {
  if (typeof window === 'undefined') return false
  return hasAlcorProxyMarker() || isInsideFrame()
}

function embeddedURL(url: string): string {
  if (!isEmbeddedInAlcor()) return url
  // 北向 API 走 Alcor 的受控代理；控制台自身 API 走 Alcor 的 console 代理前缀。
  if (url.startsWith('/api/v1/')) {
    return `/api/v2/device-farm/proxy${url}`
  }
  if (url.startsWith('/console/api/v1/')) {
    return `/api/v2/device-farm${url}`
  }
  return url
}

// Alcor 在嵌入本控制台时，通过自己的会话体系决定操作者身份与角色
// （`GET /api/v2/device-farm/session`，见 Alcor 侧 DeviceFarmSession）。
// 这个端点不消耗设备农场的 console cookie —— 也就是说嵌入模式下**不存在**
// 独立登录，控制台必须改从这里取会话，否则会一直拿到 401 并弹出登录页。
//
// 注意：该端点不在 `embeddedURL` 的翻译范围内（它本身就是 Alcor 的最终路径），
// 因此由本函数直接请求 Alcor 同源地址，不经过 deviceFarmFetch 的路径翻译。
export type AlcorEmbeddedSession = {
  user: { id: string; display_name: string; role: string }
  expires_at: string
}

export async function fetchAlcorEmbeddedSession(signal?: AbortSignal): Promise<AlcorEmbeddedSession | null> {
  if (!isEmbeddedInAlcor()) return null
  // 直接命中 Alcor 最终路径；若当前已带 console 前缀则沿用（保持同源）。
  const path = '/api/v2/device-farm/session'
  let response: Response
  try {
    response = await fetch(path, {
      method: 'GET',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
      signal,
    })
  } catch {
    return null
  }
  if (!response.ok) return null
  try {
    const payload = (await response.json()) as { data?: AlcorEmbeddedSession | null }
    return payload?.data ?? null
  } catch {
    return null
  }
}

function cookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`
  return document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(prefix))?.slice(prefix.length)
}

export async function deviceFarmFetch<T>(url: string, options: RequestInit): Promise<T> {  const headers = new Headers(options.headers)
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
