import { Button, Card, Col, Row, Space, Statistic, Tag, Typography } from 'antd'
import { Link } from 'react-router-dom'
import {
  ArrowRightOutlined,
  CalendarOutlined,
  CheckCircleFilled,
  CloudServerOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  FileSearchOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import {
  useListDeviceAuditEvents,
  useListDeviceHosts,
  useListDeviceImages,
  useListDevicePools,
  useListDeviceReservations,
  useListDevices,
} from '../api/generated/device-farm'
import { unwrapPage } from '../api/unwrap'

export function DashboardPage() {
  const imagesQuery = useListDeviceImages({ page: 1, page_size: 1 })
  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 1 })
  const poolsQuery = useListDevicePools({ page: 1, page_size: 1 })
  const readyDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'ready', health_status: 'healthy' })
  const busyDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'busy' })
  const quarantinedDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'quarantined' })
  const deletedDevicesQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'deleted' })
  const reservationsQuery = useListDeviceReservations({ page: 1, page_size: 1 })
  const auditQuery = useListDeviceAuditEvents({ page: 1, page_size: 1 })

  const images = unwrapPage(imagesQuery.data)
  const hosts = unwrapPage(hostsQuery.data)
  const pools = unwrapPage(poolsQuery.data)
  const readyDevices = unwrapPage(readyDevicesQuery.data)
  const busyDevices = unwrapPage(busyDevicesQuery.data)
  const quarantinedDevices = unwrapPage(quarantinedDevicesQuery.data)
  const deletedDevices = unwrapPage(deletedDevicesQuery.data)
  const reservations = unwrapPage(reservationsQuery.data)
  const audit = unwrapPage(auditQuery.data)
  const connected = ![
    imagesQuery,
    hostsQuery,
    poolsQuery,
    readyDevicesQuery,
    busyDevicesQuery,
    quarantinedDevicesQuery,
    deletedDevicesQuery,
    reservationsQuery,
    auditQuery,
  ].some((query) => query.isError)

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
          <Typography.Paragraph>集中查看设备容量、调度状态和基础设施健康，所有危险操作均写入审计。</Typography.Paragraph>
        </div>
        <Space wrap>
          <Link to="/devices"><Button size="large">查看设备</Button></Link>
          <Link to="/reservations"><Button size="large" type="primary" icon={<PlusOutlined />}>创建预约</Button></Link>
        </Space>
      </section>

      <Row gutter={[16, 16]} className="metric-grid">
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

      <Row gutter={[16, 16]} className="dashboard-lower-grid">
        <Col xs={24} xl={15}>
          <Card className="dashboard-panel" title="基础设施状态" extra={<Link to="/health-events">查看健康事件</Link>}>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><CheckCircleFilled /></span>
                <div><strong>控制面连接</strong><small>设备农场服务与控制台会话</small></div>
              </div>
              <Tag color={connected ? 'success' : 'error'}>{connected ? '正常' : '异常'}</Tag>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><DesktopOutlined /></span>
                <div><strong>执行宿主机</strong><small>当前已登记的 KVM 模拟器节点</small></div>
              </div>
              <Typography.Text strong>{hosts?.total ?? 0} 台</Typography.Text>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><CloudServerOutlined /></span>
                <div><strong>就绪设备</strong><small>历史、隔离和已删除记录不计入当前容量</small></div>
              </div>
              <Typography.Text strong>{readyDevices?.total ?? 0} 台</Typography.Text>
            </div>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card className="dashboard-panel capacity-panel" title="设备状态摘要">
            <div className="capacity-number">{readyDevices?.total ?? 0}</div>
            <Typography.Text type="secondary">台设备当前可以预约</Typography.Text>
            <div className="capacity-status-grid">
              <div><strong>{readyDevices?.total ?? 0}</strong><span>可用设备</span></div>
              <div><strong>{busyDevices?.total ?? 0}</strong><span>使用中</span></div>
              <div><strong>{quarantinedDevices?.total ?? 0}</strong><span>隔离设备</span></div>
              <div><strong>{deletedDevices?.total ?? 0}</strong><span>已删除历史</span></div>
            </div>
            <Typography.Paragraph className="capacity-explanation" type="secondary">
              隔离和已删除设备只用于故障追踪与历史审计，不计入可用数量。累计预约历史 {reservations?.total ?? 0} 条。
            </Typography.Paragraph>
            <div className="capacity-meta">
              <span><i className="dot dot-blue" /> 镜像 {images?.total ?? 0}</span>
              <span><i className="dot dot-green" /> 操作记录 {audit?.total ?? 0}</span>
            </div>
            <Link className="capacity-link" to="/devices"><FileSearchOutlined /> 查看设备分类 <ArrowRightOutlined /></Link>
          </Card>
        </Col>
      </Row>
    </div>
  )
}
