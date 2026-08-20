import { screen, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { DeviceHost } from '../api/generated/models'
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
})
