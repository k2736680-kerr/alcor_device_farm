import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import type { Device } from '../api/generated/models'
import { renderWithProviders } from '../test/renderWithProviders'
import { sampleDevices } from '../test/handlers'
import { server } from '../test/server'
import { DashboardPage } from './DashboardPage'

function devicePage(items: Device[], request: Request) {
  const search = new URL(request.url).searchParams
  const lifecycle = search.get('lifecycle_status')
  const health = search.get('health_status')
  const filtered = items.filter((device) =>
    (!lifecycle || device.lifecycle_status === lifecycle) && (!health || device.health_status === health),
  )
  return HttpResponse.json({
    request_id: 'req_dashboard',
    data: { items: filtered.slice(0, 1), page: 1, page_size: 1, total: filtered.length },
    error: null,
  })
}

describe('DashboardPage current operation view', () => {
  it('keeps history off the dashboard and shows only actionable current information', async () => {
    renderWithProviders(<DashboardPage />)

    const availableTitle = await screen.findByText('当前可用设备')
    const availableCard = availableTitle.closest('.ant-card')
    expect(availableCard).not.toBeNull()
    await waitFor(() => expect(within(availableCard as HTMLElement).getByText('1')).toBeInTheDocument())

    expect(screen.getByText('当前任务')).toBeInTheDocument()
    expect(await screen.findByText('当前没有创建或清理任务。')).toBeInTheDocument()
    expect(screen.getByText('每 30 秒自动刷新')).toBeInTheDocument()
    const quarantineWarning = screen.getByText('发现 1 台隔离设备，点击查看处理')
    expect(quarantineWarning).toBeInTheDocument()
    expect(quarantineWarning.closest('a')).toHaveAttribute('href', '/devices?view=quarantined')
    expect(screen.queryByText('已删除历史')).not.toBeInTheDocument()
    expect(screen.queryByText(/累计预约历史/)).not.toBeInTheDocument()
  })

  it('switches to five-second refresh while devices are being created or cleaned', async () => {
    const transitionalDevices: Device[] = [
      ...sampleDevices,
      {
        ...sampleDevices[0],
        id: 'device_00000000000005',
        provider_ref: 'emulator-5562',
        lifecycle_status: 'provisioning',
      },
      {
        ...sampleDevices[0],
        id: 'device_00000000000006',
        provider_ref: 'emulator-5564',
        lifecycle_status: 'recycling',
      },
    ]
    server.use(http.get('/api/v1/devices', ({ request }) => devicePage(transitionalDevices, request)))

    renderWithProviders(<DashboardPage />)

    const creatingLabel = await screen.findByText('创建中')
    const creatingStatus = creatingLabel.closest('.task-status')
    expect(creatingStatus).not.toBeNull()
    await waitFor(() => expect(within(creatingStatus as HTMLElement).getByText('1')).toBeInTheDocument())

    const cleaningLabel = screen.getByText('清理中')
    const cleaningStatus = cleaningLabel.closest('.task-status')
    expect(cleaningStatus).not.toBeNull()
    expect(within(cleaningStatus as HTMLElement).getByText('1')).toBeInTheDocument()
    expect(await screen.findByText('每 5 秒自动刷新')).toBeInTheDocument()
    expect(screen.getByText(/完成后数量会自动更新/)).toBeInTheDocument()
  })
})
