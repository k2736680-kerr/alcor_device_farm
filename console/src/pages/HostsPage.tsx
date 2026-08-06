import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDeviceHosts } from '../api/generated/device-farm'
import type { DeviceHost } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

const columns: TableColumnsType<DeviceHost> = [
  { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
  { title: '名称', dataIndex: 'name', width: 140 },
  { title: '类型', dataIndex: 'host_type', width: 130 },
  { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'online' ? 'green' : value === 'draining' ? 'orange' : 'default'}>{value}</Tag> },
  { title: '排空', dataIndex: 'draining', width: 80, render: (value: boolean) => (value ? <Tag color="orange">是</Tag> : <Tag>否</Tag>) },
  { title: '地址', dataIndex: 'address', ellipsis: true, render: (value?: string) => value ?? '-' },
  { title: '最后心跳', dataIndex: 'last_heartbeat_at', width: 160, render: (value?: string) => formatTime(value) },
  { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
]

export function HostsPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDeviceHosts({ page, page_size: pageSize })
  const result = unwrapPage<DeviceHost>(data)
  return (
    <PageTable<DeviceHost>
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
