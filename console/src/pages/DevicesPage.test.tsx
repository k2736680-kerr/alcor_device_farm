import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
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
})
