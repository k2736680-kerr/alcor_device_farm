import { Spin } from 'antd'
import type { MenuProps } from 'antd'
import { Alert, Avatar, Button, Layout, Menu, Space, Tag, Typography } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import {
  CalendarOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  FileSearchOutlined,
  LogoutOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import {
  getGetConsoleSessionQueryKey,
  useDeleteConsoleSession,
  useGetConsoleSession,
} from './api/generated/device-farm'
import { unwrapData } from './api/unwrap'
import { fetchAlcorEmbeddedSession } from './api/fetcher'
import type { AlcorEmbeddedSession } from './api/fetcher'
import { ConsoleRole } from './api/generated/models'
import type { ConsoleSession } from './api/generated/models'
import { LoginPage } from './pages/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { HostsPage } from './pages/HostsPage'
import { PoolsPage } from './pages/PoolsPage'
import { DevicesPage } from './pages/DevicesPage'
import { ReservationsPage } from './pages/ReservationsPage'
import { AuditPage } from './pages/AuditPage'
import { consoleDisplayName, roleLabel } from './api/labels'
import { RemoteControlProvider, useRemoteControl } from './remote/RemoteControlProvider'

const menuItems: MenuProps['items'] = [
  { key: '/', icon: <DashboardOutlined />, label: <NavLink to="/">运行概览</NavLink> },
  { key: '/hosts', icon: <DesktopOutlined />, label: <NavLink to="/hosts">宿主机</NavLink> },
  { key: '/pools', icon: <DatabaseOutlined />, label: <NavLink to="/pools">设备池</NavLink> },
  { key: '/devices', icon: <CloudServerOutlined />, label: <NavLink to="/devices">设备</NavLink> },
  { key: '/reservations', icon: <CalendarOutlined />, label: <NavLink to="/reservations">预约</NavLink> },
  { key: '/audit', icon: <FileSearchOutlined />, label: <NavLink to="/audit">操作审计</NavLink> },
]

const pageTitles: Record<string, string> = {
  '/': '运行概览',
  '/hosts': '宿主机',
  '/pools': '设备池',
  '/devices': '设备',
  '/reservations': '预约',
  '/audit': '操作审计',
}

export default function App() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const { data, isPending, isError } = useGetConsoleSession({ query: { retry: false } })
  const logout = useDeleteConsoleSession({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetConsoleSessionQueryKey() })
        navigate('/')
      },
    },
  })
  const session = unwrapData<ConsoleSession>(data)
  const currentTitle = pageTitles[location.pathname] ?? '设备农场管理'
  const embedded = window.self !== window.top
  // 被 Alcor 嵌入时不走设备农场自己的 console 会话（那份 401 会误弹登录页），
  // 改由 Alcor 的会话端点决定身份与角色（见 fetchAlcorEmbeddedSession 注释）。
  const [alcorSession, setAlcorSession] = useState<AlcorEmbeddedSession | null>(null)
  const [alcorSessionResolved, setAlcorSessionResolved] = useState(false)

  useEffect(() => {
    if (!embedded) {
      setAlcorSessionResolved(true)
      return
    }
    const controller = new AbortController()
    void fetchAlcorEmbeddedSession(controller.signal).then((value) => {
      if (controller.signal.aborted) return
      setAlcorSession(value)
      setAlcorSessionResolved(true)
    })
    return () => controller.abort()
  }, [embedded])

  useEffect(() => {
    if (embedded) {
      window.parent.postMessage({ type: 'alcor-device-farm-route', path: location.pathname }, window.location.origin)
    }
  }, [embedded, location.pathname])

  if (isPending || !alcorSessionResolved) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  // 嵌入模式：Alcor 会话是唯一判据，拿不到才回退登录页（属真正的异常）。
  if (embedded) {
    if (!alcorSession) {
      return <LoginPage />
    }
    const alcorRole = alcorSession.user.role
    const role: ConsoleSession['user']['role'] =
      alcorRole === 'admin' ? ConsoleRole.admin : alcorRole === 'viewer' ? ConsoleRole.viewer : ConsoleRole.operator
    return (
      <RemoteControlProvider>
        <AuthenticatedConsole
          currentTitle={currentTitle}
          displayName={consoleDisplayName(alcorSession.user.display_name)}
          logoutPending={false}
          onLogout={() => window.parent.postMessage({ type: 'alcor-device-farm-logout' }, window.location.origin)}
          session={{
            user: { id: alcorSession.user.id, display_name: alcorSession.user.display_name, role },
            expires_at: alcorSession.expires_at,
          }}
          embedded
        />
      </RemoteControlProvider>
    )
  }

  if (isError || !session) {
    return <LoginPage />
  }

  const displayName = consoleDisplayName(session.user.display_name)

  return (
    <RemoteControlProvider>
      <AuthenticatedConsole
        currentTitle={currentTitle}
        displayName={displayName}
        logoutPending={logout.isPending}
        onLogout={() => logout.mutate()}
        session={session}
        embedded={embedded}
      />
    </RemoteControlProvider>
  )
}

function AuthenticatedConsole({
  currentTitle,
  displayName,
  logoutPending,
  onLogout,
  session,
  embedded,
}: {
  currentTitle: string
  displayName: string
  logoutPending: boolean
  onLogout(): void
  session: ConsoleSession
  embedded: boolean
}) {
  const remote = useRemoteControl()

  return (
    <Layout className={embedded ? 'console-shell console-shell-embedded' : 'console-shell'}>
      {!embedded && <Layout.Sider className="console-sider" theme="light" width={232} breakpoint="lg" collapsedWidth={0} style={{ background: '#ffffff' }}>
        {!embedded && <div className="console-brand">
          <div className="console-brand-mark"><CloudServerOutlined /></div>
          <div className="console-brand-copy">
            <strong>Alcor 设备农场</strong>
            <span>Android 与 iOS</span>
          </div>
        </div>}
        {!embedded && <div className="console-nav-label">资源与调度</div>}
        <Menu className="console-menu" theme="light" mode="inline" selectedKeys={[location.pathname]} items={menuItems} />
        {!embedded && <div className="console-sider-footer">
          <SafetyCertificateOutlined />
          <span>设备域安全边界</span>
        </div>}
      </Layout.Sider>}
      <Layout>
        {!embedded && <Layout.Header className="console-header">
          <div>
            <Typography.Text type="secondary" className="console-eyebrow">设备农场控制台</Typography.Text>
            <Typography.Title level={4} className="console-page-title">
              {currentTitle}
            </Typography.Title>
          </div>
          <Space>
            <Tag className="console-role-tag" color="blue">{roleLabel(session.user.role)}</Tag>
            <Avatar size={34}>{displayName.slice(0, 1)}</Avatar>
            <Typography.Text strong>{displayName}</Typography.Text>
            <Button type="text" icon={<LogoutOutlined />} loading={logoutPending} onClick={onLogout}>
              退出
            </Button>
          </Space>
        </Layout.Header>}
        <Layout.Content className="console-content">
          {remote.device && (
            <Alert
              style={{ marginBottom: 14 }}
              type={remote.view?.status === 'connected' ? 'success' : 'info'}
              showIcon
              message={remote.view?.status === 'connected' ? `正在远控 ${remote.device.serial}` : `正在连接 ${remote.device.serial}`}
              description="远控会话会在控制台各页面间持续保活；明确点击取消连接或挂断会立即释放预约。关闭远控标签页后可以重新打开，控制台异常退出时系统会自动回收。"
              action={(
                <Space>
                  {remote.view?.url && <Button onClick={remote.reopen}>重新打开远控</Button>}
                  <Button danger loading={remote.isEnding} onClick={() => remote.end(true)}>
                    {remote.view?.status === 'connected' ? '挂断' : '取消连接'}
                  </Button>
                </Space>
              )}
            />
          )}
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/images" element={<Navigate to="/devices" replace />} />
            <Route path="/hosts" element={<HostsPage role={session.user.role} />} />
            <Route path="/pools" element={<PoolsPage role={session.user.role} />} />
            <Route path="/devices" element={<DevicesPage role={session.user.role} />} />
            <Route path="/reservations" element={<ReservationsPage role={session.user.role} userID={session.user.id} />} />
            <Route path="/health-events" element={<Navigate to="/devices?view=quarantined" replace />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Layout.Content>
      </Layout>
    </Layout>
  )
}
