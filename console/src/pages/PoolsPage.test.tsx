import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { Device, DevicePool, DevicePoolInput } from '../api/generated/models'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { PoolsPage } from './PoolsPage'

function poolResponse(data: DevicePoolInput) {
  return HttpResponse.json({
    request_id: 'req_target',
    data: {
      id: 'pool_000000000000001',
      ...data,
      status: 'active',
      created_at: '2026-08-06T00:00:00Z',
      updated_at: '2026-08-07T00:00:00Z',
    },
    error: null,
  })
}

async function openPoolEditor() {
  const user = userEvent.setup()
  renderWithProviders(<PoolsPage />)
  expect(await screen.findByText('default-android')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /配\s*置/ }))
  expect(await screen.findByRole('spinbutton', { name: '目标设备数' })).toHaveValue('2')
  expect(screen.queryByRole('spinbutton', { name: '最小预热数量' })).not.toBeInTheDocument()
  expect(screen.queryByRole('spinbutton', { name: '最大并发' })).not.toBeInTheDocument()
  return user
}

describe('PoolsPage pool capacity', () => {
  it('updates pool-wide total, warm and concurrency limits without requiring an expansion reason', async () => {
    let submitted: DevicePoolInput | undefined
    server.use(http.put('/api/v1/device-pools/:id', async ({ request }) => {
      submitted = await request.json() as DevicePoolInput
      return poolResponse(submitted)
    }))
    const user = await openPoolEditor()
    fireEvent.change(screen.getByRole('spinbutton', { name: '目标设备数' }), { target: { value: '3' } })
    expect(screen.getByText('当前 2 台，目标 3 台')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(submitted).toMatchObject({
      name: 'default-android',
      total_target: 3,
      min_ready: 3,
      max_concurrency: 3,
      reason: '',
    }))
  }, 10_000)

  it('requires a reason and a confirmation before shrinking', async () => {
    let submitted: DevicePoolInput | undefined
    server.use(http.put('/api/v1/device-pools/:id', async ({ request }) => {
      submitted = await request.json() as DevicePoolInput
      return poolResponse(submitted)
    }))
    const user = await openPoolEditor()
    fireEvent.change(screen.getByRole('spinbutton', { name: '目标设备数' }), { target: { value: '1' } })
    expect(screen.getByText('当前 2 台，目标 1 台')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存设置' }))
    expect(await screen.findByText('缩容时请填写至少 3 个字的调整原因')).toBeInTheDocument()
    expect(submitted).toBeUndefined()

    await user.type(screen.getByRole('textbox', { name: '调整原因（缩容时必填并写入审计）' }), '减少测试资源')
    await user.click(screen.getByRole('button', { name: '保存设置' }))
    expect((await screen.findAllByText('确认把设备池缩容到 1 台？')).length).toBeGreaterThan(0)
    expect(submitted).toBeUndefined()
    await user.click(screen.getByRole('button', { name: /确认缩容/ }))

    await waitFor(() => expect(submitted).toMatchObject({
      total_target: 1,
      min_ready: 1,
      max_concurrency: 1,
      reason: '减少测试资源',
    }))
  }, 10_000)

  it('allows an iOS pool target to be edited and submits one unified capacity target', async () => {
    const iosPool: DevicePool = {
      id: 'pool_ios_000000000001', name: 'default-ios', platform: 'ios', default_lease_seconds: 1800,
      max_lease_seconds: 86400, total_target: 6, min_ready: 6, max_concurrency: 6,
      base_device_id: 'device_ios_000000001', status: 'active',
      created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
    }
    const iosTemplate: Device = {
      id: 'device_ios_000000001', host_id: 'host_ios_000000000001', platform: 'ios', pool_id: iosPool.id,
      pool_name: iosPool.name, is_pool_base: true, device_kind: 'simulator', provider_type: 'appium_device_farm_ios',
      provider_ref: '00000000-0000-0000-0000-000000000001', lifecycle_mode: 'rebuild',
      serial: '00000000-0000-0000-0000-000000000001', capabilities: {
        runtimeId: 'com.apple.CoreSimulator.SimRuntime.iOS-26-3',
        deviceTypeId: 'com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro', model: 'iPhone 17 Pro',
      }, effective_runtime_profile: {}, reimage_status: 'idle', lifecycle_status: 'ready', health_status: 'healthy',
      consecutive_failures: 0, created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
    }
    let submitted: DevicePoolInput | undefined
    server.use(
      http.get('/api/v1/device-pools', () => HttpResponse.json({ request_id: 'req_ios_pools', data: { items: [iosPool], total: 1, page: 1, page_size: 20 }, error: null })),
      http.get('/api/v1/devices', ({ request }) => {
        const size = Number(new URL(request.url).searchParams.get('page_size') ?? 20)
        const items = size === 1 ? [iosTemplate] : [iosTemplate]
        return HttpResponse.json({ request_id: 'req_ios_devices', data: { items, total: 1, page: 1, page_size: size }, error: null })
      }),
      http.put('/api/v1/device-pools/:id', async ({ request }) => {
        submitted = await request.json() as DevicePoolInput
        return poolResponse(submitted)
      }),
    )
    const user = userEvent.setup()
    renderWithProviders(<PoolsPage />)
    expect(await screen.findByText('default-ios')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /配\s*置/ }))

    const target = await screen.findByRole('spinbutton', { name: '目标设备数' })
    expect(target).toBeEnabled()
    expect(target).toHaveValue('6')
    expect(screen.getByText(/沿用模板的 Mac、iOS 运行时和 iPhone 机型/)).toBeInTheDocument()
    expect(screen.getByText(/iPhone 17 Pro/)).toBeInTheDocument()

    fireEvent.change(target, { target: { value: '4' } })
    await user.type(screen.getByRole('textbox', { name: '调整原因（缩容时必填并写入审计）' }), '减少空闲设备')
    await user.click(screen.getByRole('button', { name: '保存设置' }))
    await user.click(await screen.findByRole('button', { name: /确认缩容/ }))
    await waitFor(() => expect(submitted).toMatchObject({
      platform: 'ios', total_target: 4, min_ready: 4, max_concurrency: 4,
    }))
  }, 10_000)

  it('only lists unassigned devices from the same platform when joining a pool', async () => {
    const unassignedAndroid: Device = {
      id: 'device_android_unassigned', host_id: 'host_000000000000001', platform: 'android',
      device_kind: 'emulator', provider_type: 'docker_emulator', provider_ref: 'emulator-5570',
      lifecycle_mode: 'rebuild', serial: 'emulator-5570', capabilities: {}, effective_runtime_profile: {},
      reimage_status: 'idle', lifecycle_status: 'ready', health_status: 'healthy', consecutive_failures: 0,
      created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
    }
    const assignedAndroid: Device = { ...unassignedAndroid, id: 'device_android_assigned', serial: 'emulator-5572', pool_id: 'pool_other', pool_name: '其他池' }
    const unassignedIOS: Device = {
      ...unassignedAndroid, id: 'device_ios_unassigned', platform: 'ios', device_kind: 'simulator',
      provider_type: 'appium_device_farm_ios', provider_ref: 'IOS-UNASSIGNED', serial: 'IOS-UNASSIGNED',
    }
    server.use(http.get('/api/v1/devices', ({ request }) => {
      const search = new URL(request.url).searchParams
      const current = search.get('pool_id') ? [] : [unassignedAndroid, assignedAndroid, unassignedIOS]
      return HttpResponse.json({ request_id: 'req_addable_devices', data: { items: current, total: current.length, page: 1, page_size: Number(search.get('page_size') ?? 20) }, error: null })
    }))
    const user = userEvent.setup()
    renderWithProviders(<PoolsPage />)

    expect(await screen.findByText('default-android')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /配\s*置/ }))
    await user.click(await screen.findByRole('button', { name: '加入设备' }))
    fireEvent.mouseDown(screen.getByText('选择设备（设备编号 · 设备标识）'))

    expect(await screen.findByText(/emulator-5570/)).toBeInTheDocument()
    expect(screen.queryByText(/emulator-5572/)).not.toBeInTheDocument()
    expect(screen.queryByText(/IOS-UNASSIGNED/)).not.toBeInTheDocument()
    expect(screen.getByText(/只列出尚未加入其他设备池、且与当前设备池平台一致的设备/)).toBeInTheDocument()
  })
})
