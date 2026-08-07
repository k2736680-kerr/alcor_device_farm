import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { DashboardPage } from './DashboardPage'

describe('DashboardPage current capacity', () => {
  it('shows usable and busy devices instead of counting history as current capacity', async () => {
    renderWithProviders(<DashboardPage />)

    const availableTitle = await screen.findByText('当前可用设备')
    const availableCard = availableTitle.closest('.ant-card')
    expect(availableCard).not.toBeNull()
    await waitFor(() => expect(within(availableCard as HTMLElement).getByText('1')).toBeInTheDocument())

    const busyTitle = screen.getByText('使用中设备')
    const busyCard = busyTitle.closest('.ant-card')
    expect(busyCard).not.toBeNull()
    await waitFor(() => expect(within(busyCard as HTMLElement).getByText('1')).toBeInTheDocument())

    expect(await screen.findByText('隔离设备')).toBeInTheDocument()
    expect(screen.getByText('已删除历史')).toBeInTheDocument()
    expect(screen.getByText(/隔离和已删除设备只用于故障追踪与历史审计/)).toBeInTheDocument()
    expect(screen.queryByText('预约历史')).not.toBeInTheDocument()
  })
})
