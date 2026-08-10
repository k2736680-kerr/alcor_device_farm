import { http, HttpResponse } from 'msw'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
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

  it('keeps the remote session alive while navigating away from the devices page', async () => {
    const user = userEvent.setup()
    let heartbeatRequests = 0
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace: vi.fn() },
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    server.use(http.post('/console/api/v1/devices/:id/remote-control/heartbeat', ({ params }) => {
      heartbeatRequests += 1
      return HttpResponse.json({ request_id: 'req_remote_heartbeat', data: {
        device_id: String(params.id), reservation_id: 'reservation_remote_0001', status: 'connected', heartbeat_interval_seconds: 15,
      }, error: null })
    }))
    renderWithProviders(<App />, '/devices')

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    expect(await screen.findByText('正在远控 emulator-5554')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: '仪表盘' }))
    expect(await screen.findByText('设备运行概览')).toBeInTheDocument()
    expect(screen.getByText('正在远控 emulator-5554')).toBeInTheDocument()
    window.dispatchEvent(new Event('focus'))
    await waitFor(() => expect(heartbeatRequests).toBeGreaterThan(0))
  }, 8_000)

  it('recovers and renews an active remote session after a console reload', async () => {
    let heartbeatRequests = 0
    window.sessionStorage.setItem('device-farm.remote-control-device', JSON.stringify({
      id: 'device_00000000000001',
      serial: 'emulator-5554',
    }))
    server.use(http.post('/console/api/v1/devices/:id/remote-control/heartbeat', ({ params }) => {
      heartbeatRequests += 1
      return HttpResponse.json({ request_id: 'req_remote_heartbeat', data: {
        device_id: String(params.id), reservation_id: 'reservation_remote_0001', status: 'connected', heartbeat_interval_seconds: 15,
      }, error: null })
    }))

    renderWithProviders(<App />)
    expect(await screen.findByText('正在远控 emulator-5554')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新打开远控' })).toBeInTheDocument()
    window.dispatchEvent(new Event('focus'))
    await waitFor(() => expect(heartbeatRequests).toBeGreaterThan(0))
  })

  it('performs an idempotent cleanup when starting remote control loses its response', async () => {
    const user = userEvent.setup()
    let endRequests = 0
    server.use(
      http.post('/console/api/v1/devices/:id/remote-control', () => HttpResponse.json({
        request_id: 'req_remote_timeout',
        data: null,
        error: { code: 'REMOTE_CONTROL_TIMEOUT', message: 'remote control request timed out', retryable: true },
      }, { status: 504 })),
      http.delete('/console/api/v1/devices/:id/remote-control', () => {
        endRequests += 1
        return HttpResponse.json({ request_id: 'req_remote_end', data: {
          device_id: 'device_00000000000001', reservation_id: 'reservation_remote_0001', status: 'ended', heartbeat_interval_seconds: 15,
        }, error: null })
      }),
    )
    renderWithProviders(<App />, '/devices')

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))

    expect(await screen.findByText(/远控连接失败（REMOTE_CONTROL_TIMEOUT/)).toBeInTheDocument()
    await waitFor(() => expect(endRequests).toBe(1))
    expect(screen.queryByText(/正在连接 emulator-5554/)).not.toBeInTheDocument()
  })

  it('treats a missing device during hangup as an already-ended session', async () => {
    const user = userEvent.setup()
    const popup = {
      closed: false,
      close: vi.fn(() => { popup.closed = true }),
      document: { title: '', body: { textContent: '' } },
      location: { replace: vi.fn() },
    }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    server.use(http.delete('/console/api/v1/devices/:id/remote-control', () => HttpResponse.json({
      request_id: 'req_missing_device',
      data: null,
      error: { code: 'NOT_FOUND', message: 'device not found', retryable: false },
    }, { status: 404 })))
    renderWithProviders(<App />, '/devices')

    const row = (await screen.findByText('emulator-5554')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row as HTMLElement).getByRole('button', { name: '远程连接' }))
    expect(await screen.findByText('正在远控 emulator-5554')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: /挂\s*断/ })[0])

    expect(await screen.findByText('远控会话已结束，设备状态已刷新')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('正在远控 emulator-5554')).not.toBeInTheDocument())
    expect(screen.queryByText(/挂断失败/)).not.toBeInTheDocument()
  })
})
