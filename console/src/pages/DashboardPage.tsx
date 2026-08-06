import { Button, Card, Col, Progress, Row, Space, Statistic, Tag, Typography } from 'antd'
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
  const devicesQuery = useListDevices({ page: 1, page_size: 1 })
  const reservationsQuery = useListDeviceReservations({ page: 1, page_size: 1 })
  const auditQuery = useListDeviceAuditEvents({ page: 1, page_size: 1 })

  const images = unwrapPage(imagesQuery.data)
  const hosts = unwrapPage(hostsQuery.data)
  const pools = unwrapPage(poolsQuery.data)
  const devices = unwrapPage(devicesQuery.data)
  const reservations = unwrapPage(reservationsQuery.data)
  const audit = unwrapPage(auditQuery.data)
  const connected = ![imagesQuery, hostsQuery, poolsQuery, devicesQuery, reservationsQuery, auditQuery].some((query) => query.isError)

  const items = [
    { title: '设备', value: devices?.total ?? 0, note: '当前纳管资源', to: '/devices', icon: <CloudServerOutlined />, tone: 'blue' },
    { title: '宿主机', value: hosts?.total ?? 0, note: 'KVM 执行节点', to: '/hosts', icon: <DesktopOutlined />, tone: 'cyan' },
    { title: '设备池', value: pools?.total ?? 0, note: '调度资源池', to: '/pools', icon: <DatabaseOutlined />, tone: 'violet' },
    { title: '预约', value: reservations?.total ?? 0, note: '设备占用记录', to: '/reservations', icon: <CalendarOutlined />, tone: 'orange' },
  ]

  return (
    <div className="dashboard-page">
      <section className="dashboard-hero">
        <div>
          <div className="dashboard-kicker"><span /> DEVICE OPERATIONS</div>
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
                <div><strong>控制面连接</strong><small>Device Farm API 与控制台会话</small></div>
              </div>
              <Tag color={connected ? 'success' : 'error'}>{connected ? '正常' : '异常'}</Tag>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><DesktopOutlined /></span>
                <div><strong>执行宿主机</strong><small>当前已登记的 KVM / Emulator 节点</small></div>
              </div>
              <Typography.Text strong>{hosts?.total ?? 0} 台</Typography.Text>
            </div>
            <div className="health-row">
              <div className="health-copy">
                <span className="health-icon"><CloudServerOutlined /></span>
                <div><strong>设备资源</strong><small>纳管设备与固定暖池容量</small></div>
              </div>
              <Typography.Text strong>{devices?.total ?? 0} 台</Typography.Text>
            </div>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card className="dashboard-panel capacity-panel" title="资源摘要">
            <div className="capacity-number">{devices?.total ?? 0}</div>
            <Typography.Text type="secondary">已纳管设备</Typography.Text>
            <Progress percent={devices?.total ? 100 : 0} showInfo={false} strokeColor="#2563eb" trailColor="#e8eef8" />
            <div className="capacity-meta">
              <span><i className="dot dot-blue" /> 镜像 {images?.total ?? 0}</span>
              <span><i className="dot dot-green" /> 审计 {audit?.total ?? 0}</span>
            </div>
            <Link className="capacity-link" to="/audit"><FileSearchOutlined /> 查看设备域审计 <ArrowRightOutlined /></Link>
          </Card>
        </Col>
      </Row>
    </div>
  )
}
