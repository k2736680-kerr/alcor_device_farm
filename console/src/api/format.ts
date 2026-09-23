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

/** Prefer the public CoreSimulator device type over Apple's internal model identifier. */
export function iosDeviceModelLabel(capabilities: Record<string, unknown>): string {
  const deviceTypeID = capabilities.deviceTypeId
  if (typeof deviceTypeID === 'string' && deviceTypeID.trim()) {
    const identifier = deviceTypeID.split('.').at(-1) ?? deviceTypeID
    return identifier.replaceAll('-', ' ')
  }
  const deviceName = capabilities.deviceName
  if (typeof deviceName === 'string' && deviceName.trim() && !deviceName.startsWith('Alcor-DF-')) {
    return deviceName
  }
  const model = capabilities.model
  return typeof model === 'string' && model.trim() ? model : 'iPhone'
}

/** Render a friendly iOS version from either the reported version or runtime id. */
export function iosVersionLabel(capabilities: Record<string, unknown>): string {
  const platformVersion = capabilities.platformVersion
  if (typeof platformVersion === 'string' && platformVersion.trim()) return `iOS ${platformVersion}`
  const runtime = capabilities.runtimeId
  if (typeof runtime === 'string') {
    const marker = runtime.match(/iOS[-.]([0-9-]+)$/i)?.[1]
    if (marker) return `iOS ${marker.replaceAll('-', '.')}`
  }
  return 'iOS'
}

/** Same as {@link iosVersionLabel} but keeps a readable fallback when nothing was reported. */
export function iosSystemVersionLabel(capabilities: Record<string, unknown>): string {
  const platformVersion = capabilities.platformVersion
  if (typeof platformVersion === 'string' && platformVersion.trim()) return `iOS ${platformVersion}`
  const runtime = capabilities.runtimeId
  if (typeof runtime === 'string') {
    const marker = runtime.match(/iOS[-.]([0-9-]+)$/i)?.[1]
    if (marker) return `iOS ${marker.replaceAll('-', '.')}`
  }
  return 'iOS（版本待上报）'
}
