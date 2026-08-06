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

function cookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`
  return document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(prefix))?.slice(prefix.length)
}

export async function deviceFarmFetch<T>(url: string, options: RequestInit): Promise<T> {
  const headers = new Headers(options.headers)
  const method = (options.method ?? 'GET').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    const csrf = cookie('device_farm_csrf')
    if (csrf) headers.set('X-CSRF-Token', decodeURIComponent(csrf))
  }
  const response = await fetch(url, { ...options, headers, credentials: 'same-origin' })
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
}
