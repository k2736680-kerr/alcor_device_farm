import { Button, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { useListDeviceAuditEvents } from '../api/generated/device-farm'
import type { AuditEvent } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { actorTypeLabel, auditActionLabel, resourceTypeLabel } from '../api/labels'
import { detailText } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { PageQueryError, ResourceDetailDrawer, ResourcePageHeader } from '../components/ResourcePage'

const actorColor: Record<string, string> = {
  console: 'blue',
  service: 'purple',
  agent: 'cyan',
  system: 'default',
}

export function AuditPage() {
  const [detailEvent, setDetailEvent] = useState<AuditEvent | null>(null)
  const columns: TableColumnsType<AuditEvent> = [
    {
      title: '时间 / 请求', dataIndex: 'created_at', width: 200, render: (value: string, event) => (
        <div className="primary-resource">
          <Typography.Text>{formatTime(value)}</Typography.Text>
          <small>{shortID(event.request_id)}</small>
        </div>
      ),
    },
    {
      title: '操作者', dataIndex: 'actor_id', width: 180, render: (value: string, event) => (
        <div className="primary-resource">
          <Typography.Text>{shortID(value)}</Typography.Text>
          <small><Tag color={actorColor[event.actor_type] ?? 'default'}>{actorTypeLabel(event.actor_type)}</Tag></small>
        </div>
      ),
    },
    {
      title: '操作内容 / 资源', dataIndex: 'resource_id', width: 320, render: (value: string, event) => (
        <div className="primary-resource">
          <Typography.Text strong>{auditActionLabel(event.action)}</Typography.Text>
          <small>{resourceTypeLabel(event.resource_type)} · {shortID(value)}</small>
        </div>
      ),
    },
    { title: '操作原因', dataIndex: 'reason', ellipsis: true, render: (value?: string) => value ?? <Typography.Text type="secondary">系统自动操作</Typography.Text> },
    { title: '详情', key: 'detail', width: 90, fixed: 'right', render: (_, event) => <Button size="small" onClick={() => setDetailEvent(event)}>查看</Button> },
  ]
  const { page, pageSize, onPageChange } = useServerPage()
  const query = useListDeviceAuditEvents(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<AuditEvent>(query.data)
  return (
    <Space direction="vertical" size={14} style={{ display: 'flex' }}>
      <ResourcePageHeader
        title="操作审计"
        description="按时间追踪设备域内的人工操作和系统自动处理。主表保留排障所需线索，完整编号和变更内容可在详情中复制。"
        dataUpdatedAt={query.dataUpdatedAt}
        isFetching={query.isFetching}
        onRefresh={() => void query.refetch()}
        autoRefreshText="每 10 秒自动更新"
      />
      {query.isError && <PageQueryError error={query.error} onRetry={() => void query.refetch()} />}
      <PageTable<AuditEvent>
        columns={columns}
        dataSource={result?.items}
        loading={query.isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: '暂无操作审计记录' }}
      />
      <ResourceDetailDrawer
        open={detailEvent !== null}
        title={detailEvent ? `审计详情 · ${auditActionLabel(detailEvent.action)}` : '审计详情'}
        onClose={() => setDetailEvent(null)}
        items={detailEvent ? [
          { key: 'time', label: '操作时间', children: formatTime(detailEvent.created_at) },
          { key: 'actor', label: '操作者', children: `${actorTypeLabel(detailEvent.actor_type)} · ${detailEvent.actor_id}` },
          { key: 'action', label: '操作内容', children: auditActionLabel(detailEvent.action) },
          { key: 'resource', label: '资源', children: `${resourceTypeLabel(detailEvent.resource_type)} · ${detailEvent.resource_id}` },
          { key: 'reason', label: '操作原因', children: detailEvent.reason ?? '系统自动操作' },
          { key: 'request', label: '完整请求编号', children: <Typography.Text code copyable>{detailEvent.request_id}</Typography.Text> },
          { key: 'event', label: '完整审计编号', children: <Typography.Text code copyable>{detailEvent.id}</Typography.Text> },
          { key: 'summary', label: '变更详情', children: detailText(detailEvent.summary) },
        ] : []}
      />
    </Space>
  )
}
