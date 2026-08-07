import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { sampleDevices } from '../test/handlers'
import { server } from '../test/server'
import { DevicesPage } from './DevicesPage'

describe('DevicesPage device categories', () => {
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
