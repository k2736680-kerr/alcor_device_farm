import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { LoginPage } from './LoginPage'
import { renderWithProviders } from '../test/renderWithProviders'
import { server } from '../test/server'

describe('LoginPage', () => {
  it('renders the login form with user id and password fields', () => {
    renderWithProviders(<LoginPage />)
    expect(screen.getByText('设备农场控制台登录')).toBeInTheDocument()
    expect(screen.getByLabelText('用户 ID')).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '登 录' })).toBeInTheDocument()
  })

  it('shows an error message when the credentials are rejected', async () => {
    server.use(
      http.post('/console/api/v1/sessions', () =>
        HttpResponse.json(
          { request_id: 'req_test', data: null, error: { code: 'INVALID_CREDENTIALS', message: '用户 ID 或密码错误', retryable: false } },
          { status: 401 },
        ),
      ),
    )
    renderWithProviders(<LoginPage />)

    fireEvent.change(screen.getByLabelText('用户 ID'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrong-password' } })
    fireEvent.click(screen.getByRole('button', { name: '登 录' }))

    expect(await screen.findByText('用户 ID 或密码错误')).toBeInTheDocument()
  })

  it('does not show an error when login succeeds', async () => {
    renderWithProviders(<LoginPage />)

    fireEvent.change(screen.getByLabelText('用户 ID'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'admin-password' } })
    fireEvent.click(screen.getByRole('button', { name: '登 录' }))

    await waitFor(() => {
      expect(screen.queryByText(/用户 ID 或密码错误/)).not.toBeInTheDocument()
    })
  })
})
