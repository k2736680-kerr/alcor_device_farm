import { Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useListDeviceAuditEvents } from '../api/generated/device-farm'
import type { AuditEvent } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { actorTypeLabel, auditActionLabel, resourceTypeLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'

const actorColor: Record<string, string> = {
  console: 'blue',
  service: 'purple',
  agent: 'cyan',
  system: 'default',
}

const columns: TableColumnsType<AuditEvent> = [
  { title: '时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
  { title: '操作来源', dataIndex: 'actor_type', width: 110, render: (value: string) => <Tag color={actorColor[value] ?? 'default'}>{actorTypeLabel(value)}</Tag> },
  { title: '操作者', dataIndex: 'actor_id', width: 150, render: (value: string) => shortID(value) },
  { title: '操作内容', dataIndex: 'action', width: 180, render: (value: string) => <Typography.Text>{auditActionLabel(value)}</Typography.Text> },
  { title: '资源类型', dataIndex: 'resource_type', width: 140, render: (value: string) => resourceTypeLabel(value) },
  { title: '资源 ID', dataIndex: 'resource_id', width: 170, render: (value: string) => shortID(value) },
  { title: '请求 ID', dataIndex: 'request_id', width: 170, render: (value: string) => shortID(value) },
  { title: '原因', dataIndex: 'reason', ellipsis: true, render: (value?: string) => value ?? '-' },
  { title: '摘要', dataIndex: 'summary', ellipsis: true, render: (value: Record<string, unknown>) => (value ? JSON.stringify(value) : '-') },
]

export function AuditPage() {
  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDeviceAuditEvents({ page, page_size: pageSize })
  const result = unwrapPage<AuditEvent>(data)
  return (
    <PageTable<AuditEvent>
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
