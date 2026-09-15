import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { DeviceHost } from '../api/generated/models'
import { sampleHosts } from '../test/handlers'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { HostsPage } from './HostsPage'

describe('HostsPage platform consistency', () => {
  it('shows shared Mac CPU honestly and gives both platforms the same readiness style', async () => {
    const common = {
      host_type: 'docker_emulator' as const,
      host_arch: 'arm64', address: '10.0.0.1', status: 'online' as const, draining: false,
      created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
    }
    const hosts: DeviceHost[] = [
      {
        ...common, id: 'host_android_000000001', name: 'Android 宿主机', host_os: 'linux', host_arch: 'amd64',
        // 线上 Host Agent 的真实上报形状：只有 kvm / gpu_render，**没有** docker。
        capabilities: { kvm: true, gpu_render: true, provider_inventory_complete: true },
        capacity: { resource_model: 'dynamic_v1', cpu_cores: 12, memory_total_mb: 16000, memory_available_mb: 8000, disk_total_mb: 100000, disk_available_mb: 50000 },
        used_capacity: { cpu_cores: 4, memory_mb: 5120 },
      },
      {
        ...common, id: 'host_ios_00000000001', name: 'iOS 宿主机', host_os: 'macos', host_type: 'appium_device_farm_ios',
        capabilities: { host_readiness: { ready: true }, xcode_version: '26.3' },
        capacity: { resource_model: 'dynamic_v1', cpu_cores: 10, memory_total_mb: 24576, memory_available_mb: 9000, disk_total_mb: 900000, disk_available_mb: 500000 },
        used_capacity: { cpu_cores: 0, memory_mb: 0 },
      },
    ]
    server.use(http.get('/api/v1/device-hosts', () => HttpResponse.json({
      request_id: 'req_hosts_consistent', data: { items: hosts, total: 2, page: 1, page_size: 20 }, error: null,
    })))

    renderWithProviders(<HostsPage />)

    const androidRow = (await screen.findByText('Android 宿主机')).closest('tr')
    const iosRow = (await screen.findByText('iOS 宿主机')).closest('tr')
    expect(within(androidRow as HTMLElement).getByText('配额 4 / 12 核')).toBeInTheDocument()
    expect(within(iosRow as HTMLElement).getByText('共享使用，共 10 核')).toBeInTheDocument()
    expect(within(androidRow as HTMLElement).getByText('自动化就绪')).toHaveClass('ant-tag-green')
    expect(within(iosRow as HTMLElement).getByText('自动化就绪')).toHaveClass('ant-tag-green')
    expect(screen.queryByRole('columnheader', { name: 'iOS 运行环境' })).not.toBeInTheDocument()
  })

  it('derives Linux readiness from host_type and kvm instead of the never-reported docker capability', async () => {
    // 回归背景：旧判定要求 capabilities.docker === true，但 Agent 心跳探针
    // （internal/hostcapacity/system.go 的 Snapshot）只上报 kvm / gpu_render，
    // 于是每台新登记的 Linux 宿主都被永久标成"自动化未就绪"（线上 55 宿主机即如此，
    // 它的 capabilities 只有 kvm + gpu_render，171 的 docker 只是历史 jsonb 残留）。
    const common = {
      host_arch: 'amd64', address: '10.0.30.55', draining: false,
      created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
    }
    const capacity = { resource_model: 'dynamic_v1' as const, cpu_cores: 12, memory_total_mb: 16000, memory_available_mb: 4000, disk_total_mb: 400000, disk_available_mb: 400000 }
    const hosts: DeviceHost[] = [
      {
        ...common, id: 'host_ready_00000000001', name: '就绪 Docker 宿主', host_os: 'linux',
        host_type: 'docker_emulator', status: 'online',
        capabilities: { kvm: true, gpu_render: true, provider_inventory_complete: true },
        capacity, used_capacity: { cpu_cores: 2, memory_mb: 7168 },
      },
      {
        ...common, id: 'host_nokvm_00000000002', name: '无 KVM 宿主', host_os: 'linux',
        host_type: 'docker_emulator', status: 'online',
        capabilities: { kvm: false, gpu_render: true },
        capacity, used_capacity: {},
      },
      {
        ...common, id: 'host_usb_00000000003', name: '真机宿主', host_os: 'linux',
        host_type: 'usb_android', status: 'online',
        capabilities: { kvm: true },
        capacity, used_capacity: {},
      },
      {
        ...common, id: 'host_offline_000000004', name: '离线宿主', host_os: 'linux',
        host_type: 'docker_emulator', status: 'offline',
        capabilities: { kvm: true },
        capacity, used_capacity: {},
      },
    ]
    server.use(http.get('/api/v1/device-hosts', () => HttpResponse.json({
      request_id: 'req_hosts_readiness', data: { items: hosts, total: hosts.length, page: 1, page_size: 20 }, error: null,
    })))

    renderWithProviders(<HostsPage />)

    const rowOf = async (name: string) => (await screen.findByText(name)).closest('tr') as HTMLElement
    expect(within(await rowOf('就绪 Docker 宿主')).getByText('自动化就绪')).toHaveClass('ant-tag-green')
    expect(within(await rowOf('无 KVM 宿主')).getByText('自动化未就绪')).toHaveClass('ant-tag-red')
    expect(within(await rowOf('真机宿主')).getByText('自动化未就绪')).toHaveClass('ant-tag-red')
    expect(within(await rowOf('离线宿主')).getByText('自动化未就绪')).toHaveClass('ant-tag-red')
  })

  it('keeps the internal address out of the main table and shows it in admin details', async () => {
    const user = userEvent.setup()
    renderWithProviders(<HostsPage />)

    const hostRow = (await screen.findByText('kvm-01')).closest('tr')
    expect(hostRow).not.toBeNull()
    expect(within(hostRow as HTMLElement).queryByText('10.0.0.1')).not.toBeInTheDocument()
    await user.click(within(hostRow as HTMLElement).getByRole('button', { name: /主机详\s*情/ }))

    const detail = await screen.findByRole('dialog', { name: /宿主机详情/ })
    expect(within(detail).getByText('内部地址')).toBeInTheDocument()
    expect(within(detail).getByText('10.0.0.1')).toBeInTheDocument()
  })

  it('edits a host name inside its existing details view', async () => {
    let submittedName = ''
    server.use(http.get('/api/v1/device-hosts/:id', () => HttpResponse.json({
      request_id: 'req_host_detail', data: sampleHosts[0], error: null,
    })))
    server.use(http.put('/api/v1/device-hosts/:id', async ({ request }) => {
      const body = await request.json() as { name: string }
      submittedName = body.name
      return HttpResponse.json({ request_id: 'req_rename_host', data: { ...sampleHosts[0], name: body.name }, error: null })
    }))
    const user = userEvent.setup()
    renderWithProviders(<HostsPage />)

    const hostRow = (await screen.findByText('kvm-01')).closest('tr')
    expect(within(hostRow as HTMLElement).queryByRole('button', { name: /改\s*名/ })).not.toBeInTheDocument()
    await user.click(within(hostRow as HTMLElement).getByRole('button', { name: /主机详\s*情/ }))
    const dialog = await screen.findByRole('dialog', { name: /宿主机详情/ })
    const input = within(dialog).getByPlaceholderText('例如：上海测试-KVM-01')
    await user.clear(input)
    await user.type(input, '上海测试-KVM-01')
    await user.click(within(dialog).getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(submittedName).toBe('上海测试-KVM-01'))
    expect(await screen.findByText(/宿主机名称已更新/)).toBeInTheDocument()
  })

  it('offers a retry and recovers after the host list fails to load', async () => {
    let attempts = 0
    server.use(http.get('/api/v1/device-hosts', () => {
      attempts += 1
      if (attempts === 1) {
        return HttpResponse.json({ error: { code: 'TEMPORARY', message: 'temporary failure' } }, { status: 503 })
      }
      return HttpResponse.json({
        request_id: 'req_hosts_retry',
        data: { items: sampleHosts, total: sampleHosts.length, page: 1, page_size: 20 },
        error: null,
      })
    }))
    const user = userEvent.setup()
    renderWithProviders(<HostsPage />)

    expect(await screen.findByText('页面数据加载失败')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /重\s*试/ }))
    expect(await screen.findByText('kvm-01')).toBeInTheDocument()
    expect(screen.queryByText('页面数据加载失败')).not.toBeInTheDocument()
  })

  it('lets admins register a host and shows only safe bootstrap guidance', async () => {
    const user = userEvent.setup()
    let submitted: Record<string, unknown> | undefined
    server.use(http.post('/api/v1/device-hosts', async ({ request }) => {
      submitted = await request.json() as Record<string, unknown>
      return HttpResponse.json({
        request_id: 'req_create_host',
        data: {
          ...sampleHosts[0],
          id: 'host_new_000000000000001',
          name: submitted.name,
          host_os: submitted.host_os,
          host_type: submitted.host_type,
          host_arch: submitted.host_arch,
          address: submitted.address,
        },
        error: null,
      }, { status: 201 })
    }))

    renderWithProviders(<HostsPage role="admin" />)
    await user.click(await screen.findByRole('button', { name: '登记宿主机' }))
    const dialog = await screen.findByRole('dialog', { name: '登记宿主机' })
    await user.type(within(dialog).getByLabelText('宿主机名称'), '北京 KVM 02')
    const arch = within(dialog).getByLabelText('架构')
    await user.clear(arch)
    await user.type(arch, 'amd64')
    await user.type(within(dialog).getByLabelText('内网地址'), '10.0.30.172')
    await user.click(within(dialog).getByRole('button', { name: /登\s*记/ }))

    await waitFor(() => expect(submitted).toMatchObject({
      name: '北京 KVM 02', host_os: 'linux', host_type: 'docker_emulator', host_arch: 'amd64', address: '10.0.30.172',
      capabilities: {}, capacity: { resource_model: 'dynamic_v1', device_slots: 1 },
    }))
    const result = await screen.findByRole('dialog', { name: '宿主机已登记' })
    expect(within(result).getByText('host_new_000000000000001')).toBeInTheDocument()
    expect(within(result).getByText('http://10.0.80.220:18182')).toBeInTheDocument()
    expect(within(result).getByText(/安全令牌=从受控 Secret 注入/)).toBeInTheDocument()
    expect(within(result).queryByText(/sk-[A-Za-z0-9]{12,}|eyJ[A-Za-z0-9_-]{20,}/)).not.toBeInTheDocument()
  })

  it('does not expose host registration or maintenance controls to viewers', async () => {
    renderWithProviders(<HostsPage role="viewer" />)
    expect(await screen.findByText('kvm-01')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '登记宿主机' })).not.toBeInTheDocument()
    const hostRow = screen.getByText('kvm-01').closest('tr') as HTMLElement
    expect(within(hostRow).getByRole('button', { name: /主机详\s*情/ })).toBeInTheDocument()
    expect(within(hostRow).queryByRole('button', { name: '暂停接收任务' })).not.toBeInTheDocument()
  })

  it('explains that pausing a host protects active work during maintenance', async () => {
    const user = userEvent.setup()
    renderWithProviders(<HostsPage role="admin" />)
    const hostRow = (await screen.findByText('kvm-01')).closest('tr') as HTMLElement
    await user.click(within(hostRow).getByRole('button', { name: '暂停接收任务' }))
    const dialog = await screen.findByRole('dialog', { name: /暂停接收任务/ })
    expect(within(dialog).getByText(/不再接收新设备创建和新预约/)).toBeInTheDocument()
    expect(within(dialog).getByText(/已有预约不会被中断/)).toBeInTheDocument()
  })
})
