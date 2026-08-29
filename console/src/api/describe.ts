/**
 * Turn raw device-domain identifiers into operator-facing names.
 *
 * The console must never present a bare ULID/UUID as the only description of a
 * resource: an administrator picking a scale-out template has to see which
 * Android/iOS version and which machine the template actually represents.
 */
import type { Device, DeviceHost, DeviceImage, DevicePool } from './generated/models'
import {
  androidVersionLabel,
  iosDeviceModelLabel,
  iosSystemVersionLabel,
  shortID,
} from './format'

/** Cross-reference lookups built from the lists a page already fetches. */
export interface ResourceLookups {
  imageByID?: ReadonlyMap<string, DeviceImage>
  hostByID?: ReadonlyMap<string, DeviceHost>
  deviceByID?: ReadonlyMap<string, Device>
  poolByID?: ReadonlyMap<string, DevicePool>
}

/** Model name (or hardware profile) used as a device's display title. */
export function deviceModelLabel(device: Device): string {
  const capabilities = device.capabilities as Record<string, unknown>
  if (device.platform === 'ios') {
    return iosDeviceModelLabel(capabilities)
  }
  const profileName = capabilities.hardware_profile_name
  if (typeof profileName === 'string' && profileName.trim()) {
    return profileName
  }
  const profileID = capabilities.hardware_profile_id
  if (typeof profileID === 'string' && profileID.trim()) {
    return profileID
  }
  return 'Android 模拟器'
}

/**
 * Operating system actually running on the device.
 * Android prefers the bound image because device-reported capabilities go stale.
 */
export function deviceSystemLabel(device: Device, imageByID?: ReadonlyMap<string, DeviceImage>): string {
  if (device.platform === 'ios') {
    return iosSystemVersionLabel(device.capabilities as Record<string, unknown>)
  }
  const image = device.image_id ? imageByID?.get(device.image_id) : undefined
  if (image) {
    return androidVersionLabel(image.api_level)
  }
  return androidVersionLabel((device.capabilities as Record<string, unknown>).apiLevel)
}

/** `Pixel 9 · Android 14（API 34）` — the shortest string that identifies a device. */
export function deviceHeadline(device: Device, lookups: ResourceLookups = {}): string {
  const model = deviceModelLabel(device)
  const system = deviceSystemLabel(device, lookups.imageByID)
  return system && system !== '-' ? `${model} · ${system}` : model
}

/** Host name with the shortened identifier as fallback. */
export function hostLabel(hostID?: string, hostByID?: ReadonlyMap<string, DeviceHost>): string {
  if (!hostID) return '-'
  return hostByID?.get(hostID)?.name ?? shortID(hostID)
}

/** Host operating system shown next to its name. */
export function hostOSLabel(host?: DeviceHost): string {
  if (!host) return ''
  if (host.host_os === 'macos') return 'macOS'
  if (host.host_os === 'linux') return 'Linux'
  return host.host_os
}

/** `Pixel 9 · emulator-5554` for cross references, or the shortened id when unknown. */
export function deviceLabel(deviceID?: string, deviceByID?: ReadonlyMap<string, Device>): string {
  if (!deviceID) return '-'
  const device = deviceByID?.get(deviceID)
  if (!device) return shortID(deviceID)
  return `${deviceModelLabel(device)} · ${device.serial}`
}

/** Pool name with the shortened identifier as fallback. */
export function poolLabel(poolID?: string, poolByID?: ReadonlyMap<string, DevicePool>): string {
  if (!poolID) return '-'
  return poolByID?.get(poolID)?.name ?? shortID(poolID)
}

/** Human name for an audit target, falling back to the shortened identifier. */
export function auditTargetLabel(
  resourceType: string,
  resourceID: string,
  lookups: ResourceLookups,
): string {
  if (resourceType === 'device_pool') return poolLabel(resourceID, lookups.poolByID)
  if (resourceType === 'device') return deviceLabel(resourceID, lookups.deviceByID)
  if (resourceType === 'device_image') return lookups.imageByID?.get(resourceID)?.name ?? shortID(resourceID)
  if (resourceType === 'device_host') return hostLabel(resourceID, lookups.hostByID)
  return shortID(resourceID)
}
