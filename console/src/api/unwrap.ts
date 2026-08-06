/**
 * Unwrap helpers for the Orval-generated client.
 *
 * The generated hooks type their responses as discriminated unions keyed by
 * HTTP status, but the runtime payload is always the plain envelope
 * `{ request_id, data, error }`. These helpers recover the `data` field
 * without fighting the union types.
 */

export interface PageResult<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

export function unwrapPage<T>(data: unknown): PageResult<T> | undefined {
  const envelope = data as { data?: PageResult<T> } | undefined
  const page = envelope?.data
  if (!page || !Array.isArray(page.items)) {
    return undefined
  }
  return page
}

export function unwrapData<T>(data: unknown): T | undefined {
  const envelope = data as { data?: T } | undefined
  return envelope?.data
}
