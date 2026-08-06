import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDevicePools } from '../api/generated/device-farm'
import type { DevicePool } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

const columns: TableColumnsType<DevicePool> = [
  { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
  { title: '名称', dataIndex: 'name', width: 180 },
  { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag> },
  { title: '默认租期(s)', dataIndex: 'default_lease_seconds', width: 120 },
  { title: '最长租期(s)', dataIndex: 'max_lease_seconds', width: 120 },
  { title: '最大并发', dataIndex: 'max_concurrency', width: 100 },
  { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
]

export function PoolsPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDevicePools({ page, page_size: pageSize })
  const result = unwrapPage<DevicePool>(data)
  return (
    <PageTable<DevicePool>
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
