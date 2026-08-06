import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDevices } from '../api/generated/device-farm'
import type { Device } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

const lifecycleColor: Record<string, string> = {
  ready: 'green',
  reserved: 'blue',
  busy: 'cyan',
  provisioning: 'orange',
  booting: 'orange',
  recycling: 'purple',
  quarantined: 'red',
  stopped: 'default',
  deleted: 'default',
}

const healthColor: Record<string, string> = {
  healthy: 'green',
  degraded: 'orange',
  unhealthy: 'red',
  unknown: 'default',
}

const columns: TableColumnsType<Device> = [
  { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
  { title: '序列号', dataIndex: 'serial', ellipsis: true, render: (value: string) => shortID(value) },
  { title: '类型', dataIndex: 'device_kind', width: 90 },
  { title: '提供方', dataIndex: 'provider_type', width: 130 },
  { title: '生命周期', dataIndex: 'lifecycle_status', width: 110, render: (value: string) => <Tag color={lifecycleColor[value] ?? 'default'}>{value}</Tag> },
  { title: '健康', dataIndex: 'health_status', width: 90, render: (value: string) => <Tag color={healthColor[value] ?? 'default'}>{value}</Tag> },
  { title: '模式', dataIndex: 'lifecycle_mode', width: 110 },
  { title: '宿主机', dataIndex: 'host_id', width: 150, render: (value: string) => shortID(value) },
  { title: 'ADB', dataIndex: 'adb_endpoint', ellipsis: true, render: (value?: string) => value ?? '-' },
  { title: 'Appium', dataIndex: 'appium_endpoint', ellipsis: true, render: (value?: string) => value ?? '-' },
  { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
]

export function DevicesPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDevices({ page, page_size: pageSize })
  const result = unwrapPage<Device>(data)
  return (
    <PageTable<Device>
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
