import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDeviceReservations } from '../api/generated/device-farm'
import type { Reservation } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

const statusColor: Record<string, string> = {
  active: 'green',
  pending: 'orange',
  released: 'default',
  expired: 'default',
  failed: 'red',
  force_released: 'purple',
}

const columns: TableColumnsType<Reservation> = [
  { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
  { title: '状态', dataIndex: 'status', width: 110, render: (value: string) => <Tag color={statusColor[value] ?? 'default'}>{value}</Tag> },
  { title: 'owner_type', dataIndex: 'owner_type', width: 110 },
  { title: 'owner_id', dataIndex: 'owner_id', width: 170, render: (value: string) => shortID(value) },
  { title: '设备池', dataIndex: 'pool_id', width: 150, render: (value: string) => shortID(value) },
  { title: '设备', dataIndex: 'device_id', width: 150, render: (value?: string) => (value ? shortID(value) : '-') },
  { title: '租期(s)', dataIndex: 'lease_seconds', width: 90 },
  { title: '开始', dataIndex: 'starts_at', width: 160, render: (value?: string) => formatTime(value) },
  { title: '到期', dataIndex: 'expires_at', width: 160, render: (value?: string) => formatTime(value) },
  { title: '释放', dataIndex: 'released_at', width: 160, render: (value?: string) => formatTime(value) },
  { title: '失败原因', dataIndex: 'failure_code', width: 130, render: (value?: string) => value ?? '-' },
]

export function ReservationsPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDeviceReservations({ page, page_size: pageSize })
  const result = unwrapPage<Reservation>(data)
  return (
    <PageTable<Reservation>
      columns={columns}
      dataSource={result?.items}
      loading={isFetching}
      total={result?.total ?? 0}
      page={result?.page ?? page}
      pageSize={result?.page_size ?? pageSize}
      onPageChange={onPageChange}
    />
  )
}
