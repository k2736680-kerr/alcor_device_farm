import { http, HttpResponse } from 'msw'
import type { Device, DeviceHost, DeviceImage, DevicePool, DevicePoolImage, Reservation, AuditEvent, HealthEventRecord, RemoteControl } from '../api/generated/models'

/** Envelope matching the real backend: { request_id, data, error }. */
function pageEnvelope<T>(items: T[], total: number, page = 1, pageSize = 20) {
  return {
    request_id: 'req_test',
    data: { items, page, page_size: pageSize, total },
    error: null,
  }
}

export const sessionEnvelope = {
  request_id: 'req_test',
  data: { user: { id: 'admin', display_name: '测试管理员', role: 'admin' as const }, expires_at: new Date(Date.now() + 3_600_000).toISOString() },
  error: null,
}

export const sampleImages: DeviceImage[] = [
  {
    id: 'image_00000000000001', name: 'android-14', docker_image: 'registry.example/alcor/android-emulator:api34',
    docker_digest: 'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
    api_level: 34, abi: 'x86_64', resolution: '1080x2400', resource_config: {}, status: 'ready',
    created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
  {
    id: 'image_00000000000002', name: 'android-15', docker_digest: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    api_level: 35, abi: 'arm64-v8a', resolution: '1080x2400', resource_config: {}, status: 'failed', validation_error: 'IMAGE_REFERENCE_REQUIRED',
    created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
]

export const sampleHosts: DeviceHost[] = [
  {
    id: 'host_000000000000001', name: 'kvm-01', host_type: 'docker_emulator', address: '10.0.0.1',
    capabilities: {}, capacity: {}, used_capacity: {}, status: 'online', draining: false,
    created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
]

export const samplePools: DevicePool[] = [
  {
    id: 'pool_000000000000001', name: 'default-android', default_lease_seconds: 1800, max_lease_seconds: 7200,
    total_target: 2, min_ready: 2, max_concurrency: 2, default_image_id: 'image_00000000000001',
    status: 'active', created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
]

export const samplePoolImages: DevicePoolImage[] = [
  {
    pool_id: 'pool_000000000000001', image_id: 'image_00000000000001', min_ready: 2, max_instances: 2,
    enabled: true, created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
]

export const sampleDevices: Device[] = [
  {
    id: 'device_00000000000001', host_id: 'host_000000000000001', device_kind: 'emulator', provider_type: 'docker_emulator',
    provider_ref: 'emulator-5554', lifecycle_mode: 'rebuild', serial: 'emulator-5554', capabilities: {},
    lifecycle_status: 'ready', health_status: 'healthy', consecutive_failures: 0,
    created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
  {
    id: 'device_00000000000002', host_id: 'host_000000000000001', device_kind: 'emulator', provider_type: 'docker_emulator',
    provider_ref: 'emulator-5556', lifecycle_mode: 'rebuild', serial: 'emulator-5556', capabilities: {},
    lifecycle_status: 'busy', health_status: 'healthy', consecutive_failures: 0,
    created_at: '2026-08-06T00:01:00Z', updated_at: '2026-08-06T00:01:00Z',
  },
  {
    id: 'device_00000000000003', host_id: 'host_000000000000001', device_kind: 'emulator', provider_type: 'docker_emulator',
    provider_ref: 'emulator-5558', lifecycle_mode: 'rebuild', serial: 'emulator-5558', capabilities: {},
    lifecycle_status: 'quarantined', health_status: 'unhealthy', health_reason: 'health check failed', consecutive_failures: 3,
    created_at: '2026-08-06T00:02:00Z', updated_at: '2026-08-06T00:02:00Z',
  },
  {
    id: 'device_00000000000004', host_id: 'host_000000000000001', device_kind: 'emulator', provider_type: 'docker_emulator',
    provider_ref: 'emulator-5560', lifecycle_mode: 'rebuild', serial: 'emulator-5560', capabilities: {},
    lifecycle_status: 'deleted', health_status: 'unhealthy', consecutive_failures: 0,
    created_at: '2026-08-06T00:03:00Z', updated_at: '2026-08-06T00:03:00Z',
  },
]

export const sampleReservations: Reservation[] = [
  {
    id: 'reservation_000000000001', pool_id: 'pool_000000000000001', owner_type: 'manual', owner_id: 'alcor-user-01',
    requested_capabilities: {}, lease_seconds: 1800, status: 'active',
    starts_at: '2026-08-06T00:00:00Z', expires_at: '2026-08-06T01:00:00Z',
    created_at: '2026-08-06T00:00:00Z', updated_at: '2026-08-06T00:00:00Z',
  },
]

export const sampleAuditEvents: AuditEvent[] = [
  {
    id: 'audit_00000000000001', actor_type: 'console', actor_id: 'admin', action: 'console.login',
    resource_type: 'console_session', resource_id: 'session_000000000001', request_id: 'req_000000000001',
    summary: { source_address: '127.0.0.1' }, created_at: '2026-08-06T00:00:00Z',
  },
]

export const sampleHealthEvents: HealthEventRecord[] = [
  {
    id: 'health_00000000000001', device_id: 'device_00000000000001', source: 'reconciler', event_type: 'adopted',
    severity: 'info', reason: 'device adopted', payload: {}, observed_at: '2026-08-06T00:00:00Z',
    created_at: '2026-08-06T00:00:00Z',
  },
]

export const sampleRemoteControl: RemoteControl = {
  device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'connected',
  url: 'http://stf.example.test/?jwt=short-lived-token#!/control/emulator-5554',
  expires_at: new Date(Date.now() + 60_000).toISOString(), heartbeat_interval_seconds: 15,
}

export const handlers = [
  // Session
  http.get('/console/api/v1/me', () => HttpResponse.json(sessionEnvelope)),
  http.post('/console/api/v1/sessions', () => HttpResponse.json(sessionEnvelope, { status: 201 })),
  http.delete('/console/api/v1/sessions/current', () => HttpResponse.json({ request_id: 'req_test', data: null, error: null })),

  // Lists
  http.get('/api/v1/device-images', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(sampleImages.slice(0, size), sampleImages.length, page, size))
  }),
  http.get('/api/v1/device-hosts', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(sampleHosts, sampleHosts.length, page, size))
  }),
  http.get('/api/v1/device-pools', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(samplePools, samplePools.length, page, size))
  }),
  http.get('/api/v1/device-pools/:id/images', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(samplePoolImages, samplePoolImages.length, page, size))
  }),
  http.get('/api/v1/devices', ({ request }) => {
    const search = new URL(request.url).searchParams
    const page = Number(search.get('page') ?? 1)
    const size = Number(search.get('page_size') ?? 20)
    const lifecycle = search.get('lifecycle_status')
    const health = search.get('health_status')
    const filtered = sampleDevices.filter((device) =>
      (!lifecycle || device.lifecycle_status === lifecycle) && (!health || device.health_status === health),
    )
    const start = (page - 1) * size
    return HttpResponse.json(pageEnvelope(filtered.slice(start, start + size), filtered.length, page, size))
  }),
  http.delete('/api/v1/devices/:id', ({ params }) => {
    const device = sampleDevices.find((item) => item.id === params.id) ?? sampleDevices[0]
    return HttpResponse.json({ request_id: 'req_delete_device', data: device, error: null }, { status: 202 })
  }),
  http.post('/console/api/v1/devices/:id/remote-control', ({ params }) => HttpResponse.json({
    request_id: 'req_remote_start', data: { ...sampleRemoteControl, device_id: String(params.id), status: 'connecting', url: undefined }, error: null,
  }, { status: 202 })),
  http.get('/console/api/v1/devices/:id/remote-control', ({ params }) => HttpResponse.json({
    request_id: 'req_remote_get', data: { ...sampleRemoteControl, device_id: String(params.id) }, error: null,
  })),
  http.post('/console/api/v1/devices/:id/remote-control/heartbeat', ({ params }) => HttpResponse.json({
    request_id: 'req_remote_heartbeat', data: { ...sampleRemoteControl, device_id: String(params.id), url: undefined }, error: null,
  })),
  http.delete('/console/api/v1/devices/:id/remote-control', ({ params }) => HttpResponse.json({
    request_id: 'req_remote_end', data: { ...sampleRemoteControl, device_id: String(params.id), status: 'ended', url: undefined }, error: null,
  })),
  http.get('/api/v1/device-reservations', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(sampleReservations, sampleReservations.length, page, size))
  }),
  http.get('/api/v1/device-audit-events', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(sampleAuditEvents, sampleAuditEvents.length, page, size))
  }),
  http.get('/api/v1/devices/:id/health-events', ({ request }) => {
    const page = Number(new URL(request.url).searchParams.get('page') ?? 1)
    const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
    return HttpResponse.json(pageEnvelope(sampleHealthEvents, sampleHealthEvents.length, page, size))
  }),
]
