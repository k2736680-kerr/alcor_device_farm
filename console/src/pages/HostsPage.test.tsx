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
        capabilities: { kvm: true, docker: true },
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

  it('keeps the internal address out of the main table and shows it in admin details', async () => {
    const user = userEvent.setup()
    renderWithProviders(<HostsPage />)

    const hostRow = (await screen.findByText('kvm-01')).closest('tr')
    expect(hostRow).not.toBeNull()
    expect(within(hostRow as HTMLElement).queryByText('10.0.0.1')).not.toBeInTheDocument()
    await user.click(within(hostRow as HTMLElement).getByRole('button', { name: /详\s*情/ }))

    const detail = await screen.findByRole('dialog', { name: /宿主机详情/ })
    expect(within(detail).getByText('内部地址')).toBeInTheDocument()
    expect(within(detail).getByText('10.0.0.1')).toBeInTheDocument()
  })

  it('renames a host with a memorable operator-facing name', async () => {
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
    await user.click(within(hostRow as HTMLElement).getByRole('button', { name: /改\s*名/ }))
    const dialog = await screen.findByRole('dialog', { name: /修改宿主机名称/ })
    const input = within(dialog).getByLabelText('宿主机显示名称')
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
})
