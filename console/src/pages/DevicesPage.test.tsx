import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { sampleDevices } from '../test/handlers'
import { server } from '../test/server'
import { DevicesPage } from './DevicesPage'

describe('DevicesPage device categories', () => {
  it('opens the selected STF control page and ends the session when the tab closes', async () => {
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
    renderWithProviders(<DevicesPage />)

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    await waitFor(() => expect(replace).toHaveBeenCalledWith(expect.stringContaining('#!/control/emulator-5554')))
    expect(screen.getByText('正在远控 emulator-5554')).toBeInTheDocument()

    popup.closed = true
    await waitFor(() => expect(endRequests).toBe(1), { timeout: 3_000 })
  }, 8_000)

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
    renderWithProviders(<DevicesPage />)

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
    renderWithProviders(<DevicesPage role="operator" />)
    expect(await screen.findByText('emulator-5554')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '远程连接' })).not.toBeInTheDocument()
  })

  it('defaults to usable devices and separates isolated and deleted records', async () => {
    const user = userEvent.setup()
    renderWithProviders(<DevicesPage />)

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
    renderWithProviders(<DevicesPage />, '/devices?view=quarantined')

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
    renderWithProviders(<DevicesPage />, '/devices?view=quarantined')

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
})
