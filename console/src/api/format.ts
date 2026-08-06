/** Render an ISO timestamp as a local date-time string, with a dash for missing values. */
export function formatTime(value?: string | null): string {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}

/** Shorten long identifiers (ULID/UUID style) for table display. */
export function shortID(value?: string | null): string {
  if (!value) {
    return '-'
  }
  return value.length > 20 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value
}
