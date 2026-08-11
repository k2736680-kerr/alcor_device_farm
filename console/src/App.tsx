import { Spin } from 'antd'
import type { MenuProps } from 'antd'
import { Alert, Avatar, Button, Layout, Menu, Space, Tag, Typography } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import {
  CalendarOutlined,
  CameraOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  FileSearchOutlined,
  HeartOutlined,
  LogoutOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import {
  getGetConsoleSessionQueryKey,
  useDeleteConsoleSession,
  useGetConsoleSession,
} from './api/generated/device-farm'
import { unwrapData } from './api/unwrap'
import type { ConsoleSession } from './api/generated/models'
import { LoginPage } from './pages/LoginPage'
import { DashboardPage } from './pages/DashboardPage'
import { ImagesPage } from './pages/ImagesPage'
import { HostsPage } from './pages/HostsPage'
import { PoolsPage } from './pages/PoolsPage'
import { DevicesPage } from './pages/DevicesPage'
import { ReservationsPage } from './pages/ReservationsPage'
import { HealthEventsPage } from './pages/HealthEventsPage'
import { AuditPage } from './pages/AuditPage'
import { consoleDisplayName, roleLabel } from './api/labels'
import { RemoteControlProvider, useRemoteControl } from './remote/RemoteControlProvider'

const menuItems: MenuProps['items'] = [
  { key: '/', icon: <DashboardOutlined />, label: <NavLink to="/">仪表盘</NavLink> },
  { key: '/hosts', icon: <DesktopOutlined />, label: <NavLink to="/hosts">宿主机</NavLink> },
  { key: '/pools', icon: <DatabaseOutlined />, label: <NavLink to="/pools">设备池</NavLink> },
  { key: '/devices', icon: <CloudServerOutlined />, label: <NavLink to="/devices">设备</NavLink> },
  { key: '/reservations', icon: <CalendarOutlined />, label: <NavLink to="/reservations">预约</NavLink> },
  { key: '/health-events', icon: <HeartOutlined />, label: <NavLink to="/health-events">健康事件</NavLink> },
  { key: '/audit', icon: <FileSearchOutlined />, label: <NavLink to="/audit">审计</NavLink> },
]

const pageTitles: Record<string, string> = {
  '/': '运行概览',
  '/images': '设备镜像',
  '/hosts': '宿主机',
  '/pools': '设备池',
  '/devices': '设备',
  '/reservations': '预约',
  '/health-events': '健康事件',
  '/audit': '审计',
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
  const currentTitle = pageTitles[location.pathname] ?? '设备资源管理'

  if (isPending) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" />
      </div>
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
}: {
  currentTitle: string
  displayName: string
  logoutPending: boolean
  onLogout(): void
  session: ConsoleSession
}) {
  const remote = useRemoteControl()

  return (
    <Layout className="console-shell">
      <Layout.Sider className="console-sider" theme="dark" width={232} breakpoint="lg" collapsedWidth={72}>
        <div className="console-brand">
          <div className="console-brand-mark"><CloudServerOutlined /></div>
          <div className="console-brand-copy">
            <strong>Alcor Farm</strong>
            <span>设备控制</span>
          </div>
        </div>
        <div className="console-nav-label">资源与调度</div>
        <Menu className="console-menu" theme="dark" mode="inline" selectedKeys={[location.pathname]} items={menuItems} />
        <div className="console-sider-footer">
          <SafetyCertificateOutlined />
          <span>设备域安全边界</span>
        </div>
      </Layout.Sider>
      <Layout>
        <Layout.Header className="console-header">
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
        </Layout.Header>
        <Layout.Content className="console-content">
          {remote.device && (
            <Alert
              style={{ marginBottom: 14 }}
              type={remote.view?.status === 'connected' ? 'success' : 'info'}
              showIcon
              message={remote.view?.status === 'connected' ? `正在远控 ${remote.device.serial}` : `正在连接 ${remote.device.serial}`}
              description="远控会话会在控制台各页面间持续保活；只有明确点击取消连接或挂断才会立即释放设备，关闭远控标签页后可重新打开，控制台异常退出时由短租约兜底回收。"
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
            <Route path="/images" element={<ImagesPage role={session.user.role} />} />
            <Route path="/hosts" element={<HostsPage />} />
            <Route path="/pools" element={<PoolsPage />} />
            <Route path="/devices" element={<DevicesPage role={session.user.role} />} />
            <Route path="/reservations" element={<ReservationsPage />} />
            <Route path="/health-events" element={<HealthEventsPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Layout.Content>
      </Layout>
    </Layout>
  )
}
