import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { Reservation } from '../api/generated/models'
import { sampleReservations } from '../test/handlers'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'
import { ReservationsPage } from './ReservationsPage'

describe('ReservationsPage platform wording and lease behavior', () => {
  it('shows a readable remaining lease and Chinese failure reason in the main table', async () => {
    const reservations: Reservation[] = [
      {
        ...sampleReservations[0],
        id: 'reservation_active_readable',
        expires_at: new Date(Date.now() + 30 * 60_000).toISOString(),
      },
      {
        ...sampleReservations[0],
        id: 'reservation_failed_readable',
        status: 'failed',
        failure_code: 'CAPACITY_UNAVAILABLE',
        starts_at: undefined,
        expires_at: undefined,
      },
    ]
    server.use(http.get('/api/v1/device-reservations', () => HttpResponse.json({
      request_id: 'req_readable_reservations',
      data: { items: reservations, total: reservations.length, page: 1, page_size: 20 },
      error: null,
    })))

    renderWithProviders(<ReservationsPage />)

    expect(await screen.findByText(/剩余 29 分钟/)).toBeInTheDocument()
    expect(screen.getByText('失败：暂无可用设备容量')).toBeInTheDocument()
  })

  it('uses the selected pool default lease and explains the sliding lease window', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ReservationsPage />)

    await user.click(await screen.findByRole('button', { name: '创建人工预约' }))
    await user.click(screen.getByLabelText('设备池'))
    await user.click(await screen.findByText(/Android · default-android · 启用/))

    expect(screen.getByRole('spinbutton', { name: '初始租期（秒）' })).toHaveValue('1800')
    expect(screen.getByText('设备池默认 30 分钟，单次租期窗口最长 2 小时。运行中的自动化可在到期前继续续租。')).toBeInTheDocument()
  })

  it('states that releasing a reservation does not erase device data', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ReservationsPage />)

    await user.click(await screen.findByRole('button', { name: /释\s*放/ }))
    expect(await screen.findByRole('dialog', { name: '释放预约 · default-android' })).toBeInTheDocument()
    expect(screen.queryByText('reservation_000000000001')).not.toBeInTheDocument()
    expect(screen.getByText('释放后预约结束；健康设备返回可用状态。设备数据不会因为释放预约而自动清空。')).toBeInTheDocument()
  })

  it('keeps internal reservation and pool ids in details instead of the daily table', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ReservationsPage />)

    const poolName = await screen.findByText('default-android')
    const row = poolName.closest('tr')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText('人工预约')).toBeInTheDocument()
    expect(within(row as HTMLElement).queryByText('reservation_000000000001')).not.toBeInTheDocument()
    expect(within(row as HTMLElement).queryByText('pool_000000000000001')).not.toBeInTheDocument()

    await user.click(within(row as HTMLElement).getByRole('button', { name: /详\s*情/ }))
    const detail = await screen.findByRole('dialog', { name: '预约详情 · default-android' })
    expect(within(detail).getByText('reservation_000000000001')).toBeInTheDocument()
    expect(within(detail).getByText('pool_000000000000001')).toBeInTheDocument()
  })

  it('asks the backend to force release a reservation owned by another client when an admin releases it', async () => {
    const user = userEvent.setup()
    let releaseBody: unknown
    server.use(http.post('/api/v1/device-reservations/:id/releases', async ({ request }) => {
      releaseBody = await request.json()
      return HttpResponse.json({ request_id: 'req_force_release', data: { ...sampleReservations[0], status: 'force_released' }, error: null })
    }))

    renderWithProviders(<ReservationsPage role="admin" userID="admin-user" />)

    await user.click(await screen.findByRole('button', { name: /释\s*放/ }))
    const dialog = await screen.findByRole('dialog', { name: '释放预约 · default-android' })
    await user.type(within(dialog).getByRole('textbox'), '管理员清理平台预约')
    await user.click(within(dialog).getByRole('button', { name: /确\s*认/ }))

    expect(releaseBody).toEqual({ reason: '管理员清理平台预约', force: true })
  })

  it('uses a normal release for the current Console owner', async () => {
    const user = userEvent.setup()
    let releaseBody: unknown
    server.use(http.post('/api/v1/device-reservations/:id/releases', async ({ request }) => {
      releaseBody = await request.json()
      return HttpResponse.json({ request_id: 'req_owner_release', data: { ...sampleReservations[0], status: 'released' }, error: null })
    }))

    renderWithProviders(<ReservationsPage role="admin" userID="alcor-user-01" />)

    await user.click(await screen.findByRole('button', { name: /释\s*放/ }))
    const dialog = await screen.findByRole('dialog', { name: '释放预约 · default-android' })
    await user.type(within(dialog).getByRole('textbox'), '本人结束远控预约')
    await user.click(within(dialog).getByRole('button', { name: /确\s*认/ }))

    expect(releaseBody).toEqual({ reason: '本人结束远控预约' })
  })
})
