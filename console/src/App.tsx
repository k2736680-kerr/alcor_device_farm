import { Spin } from 'antd'
import type { MenuProps } from 'antd'
import { Button, Layout, Menu, Space, Tag, Typography } from 'antd'
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

const menuItems: MenuProps['items'] = [
  { key: '/', icon: <DashboardOutlined />, label: <NavLink to="/">仪表盘</NavLink> },
  { key: '/images', icon: <CameraOutlined />, label: <NavLink to="/images">设备镜像</NavLink> },
  { key: '/hosts', icon: <DesktopOutlined />, label: <NavLink to="/hosts">宿主机</NavLink> },
  { key: '/pools', icon: <DatabaseOutlined />, label: <NavLink to="/pools">设备池</NavLink> },
  { key: '/devices', icon: <CloudServerOutlined />, label: <NavLink to="/devices">设备</NavLink> },
  { key: '/reservations', icon: <CalendarOutlined />, label: <NavLink to="/reservations">预约</NavLink> },
  { key: '/health-events', icon: <HeartOutlined />, label: <NavLink to="/health-events">健康事件</NavLink> },
  { key: '/audit', icon: <FileSearchOutlined />, label: <NavLink to="/audit">审计</NavLink> },
]

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

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Sider theme="dark" breakpoint="lg" collapsedWidth={64}>
        <div style={{ height: 48, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#fff', fontWeight: 600, fontSize: 15 }}>
          Alcor 设备农场
        </div>
        <Menu theme="dark" mode="inline" selectedKeys={[location.pathname]} items={menuItems} />
      </Layout.Sider>
      <Layout>
        <Layout.Header
          style={{ background: '#fff', display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingInline: 24 }}
        >
          <Typography.Text strong>设备农场控制台</Typography.Text>
          <Space>
            <Tag color="blue">{session.user.role}</Tag>
            <Typography.Text>{session.user.display_name}</Typography.Text>
            <Button size="small" icon={<LogoutOutlined />} loading={logout.isPending} onClick={() => logout.mutate()}>
              退出
            </Button>
          </Space>
        </Layout.Header>
        <Layout.Content style={{ margin: 16 }}>
          <Routes>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/images" element={<ImagesPage />} />
            <Route path="/hosts" element={<HostsPage />} />
            <Route path="/pools" element={<PoolsPage />} />
            <Route path="/devices" element={<DevicesPage />} />
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
