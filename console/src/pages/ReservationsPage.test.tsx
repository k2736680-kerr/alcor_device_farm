import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { ReservationsPage } from './ReservationsPage'

describe('ReservationsPage platform wording and lease behavior', () => {
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
    expect(screen.getByText('释放后预约结束；健康设备返回可用状态。设备数据不会因为释放预约而自动清空。')).toBeInTheDocument()
  })
})
