import { http, HttpResponse } from 'msw'
import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import App from './App'
import { renderWithProviders } from './test/renderWithProviders'
import { server } from './test/server'

describe('App session gate', () => {
  it('renders the login page when there is no session', async () => {
    server.use(
      http.get('/console/api/v1/me', () =>
        HttpResponse.json(
          { request_id: 'req_test', data: null, error: { code: 'UNAUTHENTICATED', message: 'console session is not authenticated', retryable: false } },
          { status: 401 },
        ),
      ),
    )
    renderWithProviders(<App />)
    expect(await screen.findByText('设备农场控制台登录')).toBeInTheDocument()
  })

  it('renders the console layout with navigation when a session exists', async () => {
    renderWithProviders(<App />)

    expect(await screen.findByText('测试管理员')).toBeInTheDocument()
    expect(screen.getByText('仪表盘')).toBeInTheDocument()
    expect(screen.getByText('健康事件')).toBeInTheDocument()
    // menu labels also appear as dashboard statistic titles, so expect at least one
    for (const label of ['设备镜像', '宿主机', '设备池', '设备', '预约', '审计']) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }
    expect(screen.getByRole('button', { name: /退出/ })).toBeInTheDocument()
  })
})
