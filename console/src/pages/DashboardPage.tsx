import { useEffect, useState } from 'react'
import { Button, Card, Col, Row, Space, Statistic, Tag, Typography } from 'antd'
import { Link } from 'react-router-dom'
import {
  ArrowRightOutlined,
  CalendarOutlined,
  CheckCircleFilled,
  CloudServerOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  LoadingOutlined,
  PlusOutlined,
  ReloadOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import {
  useListDeviceHosts,
  useListDevicePools,
  useListDevices,
} from '../api/generated/device-farm'
import { unwrapPage } from '../api/unwrap'

const FAST_REFRESH_MS = 5_000
const IDLE_REFRESH_MS = 30_000

export function DashboardPage() {
  const [fastPolling, setFastPolling] = useState(false)
  const deviceRefreshInterval = fastPolling ? FAST_REFRESH_MS : IDLE_REFRESH_MS
  const deviceQueryOptions = { query: { refetchInterval: deviceRefreshInterval } }
  const infrastructureQueryOptions = { query: { refetchInterval: IDLE_REFRESH_MS } }

  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 1 }, infrastructureQueryOptions)
  const poolsQuery = useListDevicePools({ page: 1, page_size: 1 }, infrastructureQueryOptions)
  const readyDevicesQuery = useListDevices(
    { page: 1, page_size: 1, lifecycle_status: 'ready', health_status: 'healthy' },
    deviceQueryOptions,
  )
  const busyDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'busy' }, deviceQueryOptions)
  const provisioningDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'provisioning' }, deviceQueryOptions)
  const bootingDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'booting' }, deviceQueryOptions)
  const recyclingDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'recycling' }, deviceQueryOptions)
  const stoppedDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'stopped' }, deviceQueryOptions)
  const quarantinedDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'quarantined' }, deviceQueryOptions)

  const hosts = unwrapPage(hostsQuery.data)
  const pools = unwrapPage(poolsQuery.data)
  const readyDevices = unwrapPage(readyDevicesQuery.data)
  const busyDevices = unwrapPage(busyDevicesQuery.data)
  const provisioningDevices = unwrapPage(provisioningDevicesQuery.data)
  const bootingDevices = unwrapPage(bootingDevicesQuery.data)
  const recyclingDevices = unwrapPage(recyclingDevicesQuery.data)
  const stoppedDevices = unwrapPage(stoppedDevicesQuery.data)
  const quarantinedDevices = unwrapPage(quarantinedDevicesQuery.data)
  const creatingCount = (provisioningDevices?.total ?? 0) + (bootingDevices?.total ?? 0)
  const cleaningCount = (recyclingDevices?.total ?? 0) + (stoppedDevices?.total ?? 0)
  const hasRunningTask = creatingCount + cleaningCount > 0
  const queries = [
    hostsQuery,
    poolsQuery,
    readyDevicesQuery,
    busyDevicesQuery,
    provisioningDevicesQuery,
    bootingDevicesQuery,
    recyclingDevicesQuery,
    stoppedDevicesQuery,
    quarantinedDevicesQuery,
  ]
  const connected = !queries.some((query) => query.isError)
  const isRefreshing = queries.some((query) => query.isFetching)
  const lastUpdatedAt = Math.max(...queries.map((query) => query.dataUpdatedAt), 0)

  useEffect(() => {
    setFastPolling(hasRunningTask)
  }, [hasRunningTask])

  const refreshAll = () => {
    void Promise.all(queries.map((query) => query.refetch()))
  }

  const items = [
    { title: '当前可用设备', value: readyDevices?.total ?? 0, note: '现在可以直接预约使用', to: '/devices', icon: <CloudServerOutlined />, tone: 'blue' },
    { title: '使用中设备', value: busyDevices?.total ?? 0, note: '正在被预约占用', to: '/reservations', icon: <CalendarOutlined />, tone: 'orange' },
    { title: '宿主机', value: hosts?.total ?? 0, note: '运行模拟器的服务器', to: '/hosts', icon: <DesktopOutlined />, tone: 'cyan' },
    { title: '设备池', value: pools?.total ?? 0, note: '设备调度分组', to: '/pools', icon: <DatabaseOutlined />, tone: 'violet' },
  ]

  return (
    <div className="dashboard-page">
      <section className="dashboard-hero">
        <div>
          <div className="dashboard-kicker"><span /> 设备运行状态</div>
          <Typography.Title level={2}>设备运行概览</Typography.Title>
          <Typography.Paragraph>只展示当前容量、进行中的任务和需要处理的异常。</Typography.Paragraph>
        </div>
        <Space wrap>
          <Button icon={<ReloadOutlined />} loading={isRefreshing} onClick={refreshAll}>刷新状态</Button>
          <Link to="/devices"><Button>查看设备</Button></Link>
          <Link to="/reservations"><Button type="primary" icon={<PlusOutlined />}>创建预约</Button></Link>
        </Space>
      </section>

      <Row gutter={[14, 14]} className="metric-grid">
        {items.map((item) => (
          <Col xs={24} sm={12} xl={6} key={item.to}>
            <Link to={item.to}>
              <Card hoverable className={`metric-card metric-${item.tone}`}>
                <div className="metric-card-top">
                  <span className="metric-icon">{item.icon}</span>
                  <ArrowRightOutlined className="metric-arrow" />
                </div>
                <Statistic title={item.title} value={item.value} />
                <Typography.Text type="secondary">{item.note}</Typography.Text>
              </Card>
            </Link>
          </Col>
        ))}
      </Row>

      <Row gutter={[14, 14]} className="dashboard-lower-grid">
        <Col xs={24} xl={13}>
          <Card className="dashboard-panel" title="基础设施状态" extra={<Link to="/health-events">查看健康事件</Link>}>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><CheckCircleFilled /></span>
                <div><strong>控制面连接</strong><small>设备农场服务与控制台通信</small></div>
              </div>
              <Tag color={connected ? 'success' : 'error'}>{connected ? '正常' : '异常'}</Tag>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><DesktopOutlined /></span>
                <div><strong>执行宿主机</strong><small>当前登记的模拟器服务器</small></div>
              </div>
              <Typography.Text strong>{hosts?.total ?? 0} 台</Typography.Text>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><CloudServerOutlined /></span>
                <div><strong>当前运行设备</strong><small>可用和使用中的设备，不包含历史记录</small></div>
              </div>
              <Typography.Text strong>{(readyDevices?.total ?? 0) + (busyDevices?.total ?? 0)} 台</Typography.Text>
            </div>
          </Card>
        </Col>
        <Col xs={24} xl={11}>
          <Card
            className="dashboard-panel task-panel"
            title="当前任务"
            extra={<span className="refresh-status">{hasRunningTask ? '每 5 秒自动刷新' : '每 30 秒自动刷新'}</span>}
          >
            <div className="task-status-grid">
              <div className={creatingCount > 0 ? 'task-status active' : 'task-status'}>
                <span className="task-status-icon"><LoadingOutlined spin={creatingCount > 0} /></span>
                <div><strong>{creatingCount}</strong><span>创建中</span></div>
              </div>
              <div className={cleaningCount > 0 ? 'task-status active' : 'task-status'}>
                <span className="task-status-icon"><ReloadOutlined spin={cleaningCount > 0} /></span>
                <div><strong>{cleaningCount}</strong><span>清理中</span></div>
              </div>
            </div>
            <div className="task-message">
              {hasRunningTask ? (
                <><LoadingOutlined spin /> 系统正在处理设备，完成后数量会自动更新，无需手动刷新。</>
              ) : (
                <><CheckCircleFilled /> 当前没有创建或清理任务。</>
              )}
            </div>
            {(quarantinedDevices?.total ?? 0) > 0 && (
              <Link className="task-warning" to="/devices">
                <WarningOutlined /> 发现 {quarantinedDevices?.total ?? 0} 台隔离设备，点击查看处理
              </Link>
            )}
            <div className="last-refresh">
              最近更新：{lastUpdatedAt > 0 ? new Date(lastUpdatedAt).toLocaleTimeString('zh-CN', { hour12: false }) : '正在加载'}
            </div>
          </Card>
        </Col>
      </Row>
    </div>
  )
}
