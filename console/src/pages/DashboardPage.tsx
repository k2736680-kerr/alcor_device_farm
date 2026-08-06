import { Card, Col, Row, Statistic } from 'antd'
import { Link } from 'react-router-dom'
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
  const images = unwrapPage(useListDeviceImages({ page: 1, page_size: 1 }).data)
  const hosts = unwrapPage(useListDeviceHosts({ page: 1, page_size: 1 }).data)
  const pools = unwrapPage(useListDevicePools({ page: 1, page_size: 1 }).data)
  const devices = unwrapPage(useListDevices({ page: 1, page_size: 1 }).data)
  const reservations = unwrapPage(useListDeviceReservations({ page: 1, page_size: 1 }).data)
  const audit = unwrapPage(useListDeviceAuditEvents({ page: 1, page_size: 1 }).data)

  const items = [
    { title: '设备镜像', value: images?.total ?? 0, to: '/images' },
    { title: '宿主机', value: hosts?.total ?? 0, to: '/hosts' },
    { title: '设备池', value: pools?.total ?? 0, to: '/pools' },
    { title: '设备', value: devices?.total ?? 0, to: '/devices' },
    { title: '预约', value: reservations?.total ?? 0, to: '/reservations' },
    { title: '审计事件', value: audit?.total ?? 0, to: '/audit' },
  ]

  return (
    <Row gutter={[16, 16]}>
      {items.map((item) => (
        <Col xs={12} md={8} xl={4} key={item.to}>
          <Link to={item.to}>
            <Card hoverable>
              <Statistic title={item.title} value={item.value} />
            </Card>
          </Link>
        </Col>
      ))}
    </Row>
  )
}
