import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '../test/renderWithProviders'
import { AuditPage } from './AuditPage'

describe('AuditPage identifier presentation', () => {
  it('keeps request and actor identifiers in details instead of the daily table', async () => {
    const user = userEvent.setup()
    renderWithProviders(<AuditPage />)

    const action = await screen.findByText('登录控制台')
    const row = action.closest('tr')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText('控制台用户')).toBeInTheDocument()
    expect(within(row as HTMLElement).queryByText('req_000000000001')).not.toBeInTheDocument()
    expect(within(row as HTMLElement).queryByText('admin')).not.toBeInTheDocument()

    await user.click(within(row as HTMLElement).getByRole('button', { name: /查\s*看/ }))
    const detail = await screen.findByRole('dialog', { name: '审计详情 · 登录控制台' })
    expect(within(detail).getByText('req_000000000001')).toBeInTheDocument()
    expect(within(detail).getByText('admin')).toBeInTheDocument()
  })
})
