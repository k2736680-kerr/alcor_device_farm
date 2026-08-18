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

/** Present the Android release together with its API level when known. */
export function androidVersionLabel(value?: unknown): string {
  const apiLevel = typeof value === 'number' ? value : Number(value)
  if (!Number.isInteger(apiLevel) || apiLevel <= 0) {
    return '-'
  }
  const releases: Record<number, string> = {
    26: '8.0', 27: '8.1', 28: '9', 29: '10', 30: '11', 31: '12', 32: '12L',
    33: '13', 34: '14', 35: '15', 36: '16',
  }
  const release = releases[apiLevel]
  return release ? `Android ${release}（API ${apiLevel}）` : `API ${apiLevel}`
}
