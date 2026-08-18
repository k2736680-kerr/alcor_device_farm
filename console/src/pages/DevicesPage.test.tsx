import { screen, waitFor, within } from '@testing-library/react'
import { focusManager } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { sampleDevices } from '../test/handlers'
import { server } from '../test/server'
import { RemoteControlProvider } from '../remote/RemoteControlProvider'
import { DevicesPage } from './DevicesPage'

function DevicesPageWithRemoteControl({
  role = 'admin',
  connectTimeoutMs,
}: {
  role?: 'viewer' | 'operator' | 'admin'
  connectTimeoutMs?: number
}) {
  return (
    <RemoteControlProvider connectTimeoutMs={connectTimeoutMs}>
      <DevicesPage role={role} />
    </RemoteControlProvider>
  )
}

describe('DevicesPage device categories', () => {
  afterEach(() => focusManager.setFocused(undefined))

  it('shows the selected image version instead of stale device capabilities', async () => {
    renderWithProviders(<DevicesPageWithRemoteControl />)
    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText('Android 14（API 34）')).toBeInTheDocument()
    expect(within(row as HTMLElement).queryByText('Android 16（API 36）')).not.toBeInTheDocument()
  })

  it('keeps the remote session until explicit hangup when the STF tab closes', async () => {
    const user = userEvent.setup()
    let endRequests = 0
    const replace = vi.fn()
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace },
      opener: window,
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    server.use(http.delete('/console/api/v1/devices/:id/remote-control', () => {
      endRequests += 1
      return HttpResponse.json({ request_id: 'req_remote_end', data: {
        device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'ended', heartbeat_interval_seconds: 15,
      }, error: null })
    }))
    renderWithProviders(<DevicesPageWithRemoteControl />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    await waitFor(() => expect(replace).toHaveBeenCalledWith(expect.stringContaining('#!/control/emulator-5554')))
    expect(within(row as HTMLElement).getByRole('button', { name: /挂\s*断/ })).toBeInTheDocument()

    popup.closed = true
    window.dispatchEvent(new Event('focus'))
    await new Promise((resolve) => window.setTimeout(resolve, 100))
    expect(endRequests).toBe(0)
    expect(within(row as HTMLElement).getByRole('button', { name: /挂\s*断/ })).toBeInTheDocument()
  })

  it('does not mistake a severed cross-origin popup handle for a closed STF tab', async () => {
    const user = userEvent.setup()
    let navigated = false
    let endRequests = 0
    const popup = {
      get closed() { return navigated },
      close: vi.fn(),
      document: { title: '', body: { textContent: '' } },
      location: { replace: vi.fn(() => { navigated = true }) },
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    server.use(http.delete('/console/api/v1/devices/:id/remote-control', () => {
      endRequests += 1
      return HttpResponse.json({ request_id: 'req_remote_end', data: {
        device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'ended', heartbeat_interval_seconds: 15,
      }, error: null })
    }))
    renderWithProviders(<DevicesPageWithRemoteControl />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    await waitFor(() => expect(navigated).toBe(true))
    await new Promise((resolve) => window.setTimeout(resolve, 1_200))
    window.dispatchEvent(new Event('focus'))
    expect(endRequests).toBe(0)
    expect(within(row as HTMLElement).getByRole('button', { name: /挂\s*断/ })).toBeInTheDocument()
  }, 8_000)

  it('continues polling and opens STF while the Console tab is in the background', async () => {
    const user = userEvent.setup()
    let statusRequests = 0
    const replace = vi.fn()
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace },
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const connecting = {
      device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'connecting', heartbeat_interval_seconds: 15,
    }
    const connected = {
      ...connecting, status: 'connected', url: 'http://stf.test/#!/control/emulator-5554',
    }
    server.use(
      http.post('/console/api/v1/devices/:id/remote-control', () => HttpResponse.json({ request_id: 'req_remote_start', data: connecting, error: null })),
      http.get('/console/api/v1/devices/:id/remote-control', () => {
        statusRequests += 1
        const data = statusRequests === 1 ? connecting : connected
        return HttpResponse.json({ request_id: 'req_remote_get', data, error: null })
      }),
    )
    focusManager.setFocused(false)
    renderWithProviders(<DevicesPageWithRemoteControl />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))

    await waitFor(() => expect(replace).toHaveBeenCalledWith(connected.url), { timeout: 3_000 })
    expect(statusRequests).toBeGreaterThanOrEqual(2)
  }, 6_000)

  it('cancels a connection that never leaves the connecting state', async () => {
    const user = userEvent.setup()
    let endRequests = 0
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace: vi.fn() },
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const connecting = {
      device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'connecting', heartbeat_interval_seconds: 15,
    }
    server.use(
      http.post('/console/api/v1/devices/:id/remote-control', () => HttpResponse.json({ request_id: 'req_remote_start', data: connecting, error: null })),
      http.get('/console/api/v1/devices/:id/remote-control', () => HttpResponse.json({ request_id: 'req_remote_get', data: connecting, error: null })),
      http.delete('/console/api/v1/devices/:id/remote-control', () => {
        endRequests += 1
        return HttpResponse.json({ request_id: 'req_remote_end', data: { ...connecting, status: 'ended' }, error: null })
      }),
    )
    renderWithProviders(<DevicesPageWithRemoteControl connectTimeoutMs={100} />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))

    expect(await screen.findByText('设备连接超时，已取消本次连接并刷新设备状态')).toBeInTheDocument()
    await waitFor(() => expect(endRequests).toBe(1))
    await waitFor(() => expect(within(row as HTMLElement).queryByRole('button', { name: /取消连接/ })).not.toBeInTheDocument())
  })

  it('closes the STF tab after the administrator hangs up', async () => {
    const user = userEvent.setup()
    const replace = vi.fn()
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace },
      opener: window,
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    renderWithProviders(<DevicesPageWithRemoteControl />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    await waitFor(() => expect(replace).toHaveBeenCalledWith(expect.stringContaining('#!/control/emulator-5554')))
    await user.click(screen.getAllByRole('button', { name: /挂\s*断/ })[0])

    await waitFor(() => expect(popup.close).toHaveBeenCalledTimes(1))
    expect(popup.opener).toBe(window)
    expect(screen.queryByText('正在远控 emulator-5554')).not.toBeInTheDocument()
  }, 8_000)

  it('does not show remote control to non-admin console roles', async () => {
    renderWithProviders(<DevicesPageWithRemoteControl role="operator" />)
    expect(await screen.findByText('emulator-5554')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '远程连接' })).not.toBeInTheDocument()
  })

  it('defaults to usable devices and separates isolated and deleted records', async () => {
    const user = userEvent.setup()
    renderWithProviders(<DevicesPageWithRemoteControl />)

    expect(await screen.findByText('emulator-5554')).toBeInTheDocument()
    expect(screen.queryByText('emulator-5558')).not.toBeInTheDocument()
    expect(screen.getByText('安卓模拟器')).toBeInTheDocument()
    expect(screen.getByText('Docker 模拟器')).toBeInTheDocument()
    expect(screen.getByText('可用')).toBeInTheDocument()
    expect(screen.getByText('正常')).toBeInTheDocument()

    await user.click(screen.getByText('隔离设备（1）'))
    const isolatedSerial = await screen.findByText('emulator-5558')
    const isolatedRow = isolatedSerial.closest('tr')
    expect(isolatedRow).not.toBeNull()
    expect(within(isolatedRow as HTMLElement).getByText('已隔离')).toBeInTheDocument()
    expect(within(isolatedRow as HTMLElement).getByText('故障')).toBeInTheDocument()

    await user.click(screen.getByText('已删除历史（1）'))
    const deletedSerial = await screen.findByText('emulator-5560')
    const deletedRow = deletedSerial.closest('tr')
    expect(deletedRow).not.toBeNull()
    expect(within(deletedRow as HTMLElement).getByText('已删除')).toBeInTheDocument()
    expect(within(deletedRow as HTMLElement).queryByRole('button')).not.toBeInTheDocument()
  })

  it('opens the isolated device list directly from the dashboard link', async () => {
    renderWithProviders(<DevicesPageWithRemoteControl />, '/devices?view=quarantined')

    expect(await screen.findByText('emulator-5558')).toBeInTheDocument()
    expect(screen.queryByText('emulator-5554')).not.toBeInTheDocument()
    expect(screen.getByText('隔离设备（1）').closest('.ant-segmented-item')).toHaveClass('ant-segmented-item-selected')
  })

  it('requires a reason and a second confirmation before deleting an isolated device', async () => {
    const user = userEvent.setup()
    let deleteRequests = 0
    server.use(http.delete('/api/v1/devices/:id', async ({ request, params }) => {
      const body = await request.json() as { reason?: string }
      expect(params.id).toBe('device_00000000000003')
      expect(body.reason).toBe('设备无法恢复，确认删除')
      deleteRequests += 1
      return HttpResponse.json({ request_id: 'req_delete_test', data: sampleDevices[2], error: null }, { status: 202 })
    }))
    renderWithProviders(<DevicesPageWithRemoteControl />, '/devices?view=quarantined')

    const isolatedSerial = await screen.findByText('emulator-5558')
    const isolatedRow = isolatedSerial.closest('tr')
    expect(isolatedRow).not.toBeNull()
    await user.click(within(isolatedRow as HTMLElement).getByRole('button', { name: /删\s*除/ }))
    await user.type(screen.getByPlaceholderText('例如：设备无法恢复，确认清理运行资源'), '设备无法恢复，确认删除')
    await user.click(screen.getByRole('button', { name: '下一步' }))

    expect((await screen.findAllByText('确认删除这台设备？')).length).toBeGreaterThan(0)
    await user.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(deleteRequests).toBe(1))
    expect(await screen.findByText(/删除任务已受理/)).toBeInTheDocument()
  })

  it('edits an idle emulator only after warning that APK and device data are erased', async () => {
    const user = userEvent.setup()
    let reimageRequests = 0
    server.use(http.post('/api/v1/devices/:id/reimages', async ({ request, params }) => {
      const body = await request.json() as { image_id: string; runtime_profile: { container_memory_mb?: number }; reason: string }
      expect(params.id).toBe('device_00000000000001')
      expect(body.image_id).toBe('image_00000000000001')
      expect(body.runtime_profile.container_memory_mb).toBe(5120)
      expect(body.reason).toBe('验证不同运行规格')
      reimageRequests += 1
      return HttpResponse.json({ request_id: 'req_reimage_test', data: { ...sampleDevices[0], lifecycle_status: 'provisioning', reimage_status: 'pending' }, error: null }, { status: 202 })
    }))
    renderWithProviders(<DevicesPageWithRemoteControl />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '编辑配置' }))
    expect(await screen.findByText('重装会清空这台模拟器里的 APK 和全部设备数据')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('例如：需要验证 Android 15 兼容性'), '验证不同运行规格')
    await user.click(screen.getByRole('button', { name: '下一步' }))
    expect((await screen.findAllByText('确认更换镜像并重装？')).length).toBeGreaterThan(0)
    await user.click(screen.getByRole('button', { name: '确认清空并重装' }))
    await waitFor(() => expect(reimageRequests).toBe(1))
    expect(await screen.findByText(/重装任务已受理/)).toBeInTheDocument()
  })

  it('shows exact Chinese memory and disk shortfalls while creation waits for host capacity', async () => {
    const user = userEvent.setup()
    server.use(
      http.post('/api/v1/device-provisionings', () => HttpResponse.json({
        request_id: 'req_capacity_wait', data: { id: 'provisioning_capacity_wait', status: 'preparing_image' }, error: null,
      }, { status: 202 })),
      http.get('/api/v1/device-provisionings/:id', ({ params }) => HttpResponse.json({
        request_id: 'req_capacity_state',
        data: {
          id: params.id,
          status: 'waiting_capacity',
          error_stage: 'host_capacity',
          error_code: 'DEVICE_CAPACITY_UNAVAILABLE',
          capacity_result: {
            fits: false,
            additional_devices: 0,
            limiting_resource: 'memory',
            shortfall: { memory_mb: 2048, disk_mb: 8192 },
            available_cpu_cores: 8,
            available_memory_mb: 3072,
            available_disk_mb: 4096,
          },
        },
        error: null,
      })),
    )
    renderWithProviders(<DevicesPageWithRemoteControl />)

    await user.click(await screen.findByRole('button', { name: '新增设备' }))
    await user.click(screen.getByRole('button', { name: '下一步' }))
    const imageRow = (await screen.findByText('Android API 36')).closest('tr')
    expect(imageRow).not.toBeNull()
    const imageRadio = within(imageRow as HTMLElement).getByRole('radio')
    await user.click(imageRadio.parentElement as HTMLElement)
    await user.click(screen.getByRole('button', { name: '下一步' }))
    await user.click(screen.getByRole('button', { name: '下一步' }))
    await user.click(screen.getByRole('button', { name: '创建设备' }))

    expect(await screen.findByText('设备创建进度：等待宿主机容量')).toBeInTheDocument()
    expect(screen.getAllByText('宿主机资源不足：内存还缺 2048 MB，磁盘还缺 8192 MB。容量恢复后会自动继续创建。').length).toBeGreaterThan(0)
    expect(screen.queryByText(/unknown|ERROR/)).not.toBeInTheDocument()
  })
})
