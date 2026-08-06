import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDeviceImages } from '../api/generated/device-farm'
import type { DeviceImage } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

const columns: TableColumnsType<DeviceImage> = [
  { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
  { title: '名称', dataIndex: 'name', width: 160 },
  { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'ready' ? 'green' : value === 'failed' ? 'red' : 'orange'}>{value}</Tag> },
  { title: 'API 级别', dataIndex: 'api_level', width: 90 },
  { title: 'ABI', dataIndex: 'abi', width: 90 },
  { title: '分辨率', dataIndex: 'resolution', width: 110 },
  { title: '镜像引用', dataIndex: 'docker_image', ellipsis: true, render: (value?: string) => value ?? '-' },
  { title: '摘要', dataIndex: 'docker_digest', ellipsis: true, render: (value: string) => shortID(value) },
  { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
]

export function ImagesPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDeviceImages({ page, page_size: pageSize })
  const result = unwrapPage<DeviceImage>(data)
  return (
    <PageTable<DeviceImage>
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
