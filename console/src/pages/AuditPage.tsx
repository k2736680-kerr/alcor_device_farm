import { Button, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useMemo, useState } from 'react'
import {
  useListDeviceAuditEvents,
  useListDeviceHosts,
  useListDeviceImages,
  useListDevicePools,
  useListDevices,
} from '../api/generated/device-farm'
import type { AuditEvent, Device, DeviceHost, DeviceImage, DevicePool } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime } from '../api/format'
import { auditTargetLabel } from '../api/describe'
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
  // 审计只保存资源编号；这里额外拉取名称映射，避免管理员只能看到一串编号。
  const poolsQuery = useListDevicePools({ page: 1, page_size: 200 }, { query: { staleTime: 60_000 } })
  const devicesQuery = useListDevices({ page: 1, page_size: 200 }, { query: { staleTime: 60_000 } })
  const imagesQuery = useListDeviceImages({ page: 1, page_size: 200 }, { query: { staleTime: 60_000 } })
  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 200 }, { query: { staleTime: 60_000 } })
  const lookups = useMemo(() => ({
    poolByID: new Map((unwrapPage<DevicePool>(poolsQuery.data)?.items ?? []).map((pool) => [pool.id, pool])),
    deviceByID: new Map((unwrapPage<Device>(devicesQuery.data)?.items ?? []).map((device) => [device.id, device])),
    imageByID: new Map((unwrapPage<DeviceImage>(imagesQuery.data)?.items ?? []).map((image) => [image.id, image])),
    hostByID: new Map((unwrapPage<DeviceHost>(hostsQuery.data)?.items ?? []).map((host) => [host.id, host])),
  }), [poolsQuery.data, devicesQuery.data, imagesQuery.data, hostsQuery.data])
  const columns: TableColumnsType<AuditEvent> = [
    {
      title: '操作时间', dataIndex: 'created_at', width: 200, render: (value: string) => (
        <div className="primary-resource">
          <Typography.Text>{formatTime(value)}</Typography.Text>
          <small>完整请求编号见详情</small>
        </div>
      ),
    },
    {
      title: '操作者', dataIndex: 'actor_id', width: 180, render: (_value: string, event) => (
        <div className="primary-resource">
          <Tag color={actorColor[event.actor_type] ?? 'default'}>{actorTypeLabel(event.actor_type)}</Tag>
          <small>完整标识见详情</small>
        </div>
      ),
    },
    {
      title: '操作内容 / 操作对象', dataIndex: 'resource_id', width: 340, render: (value: string, event) => (
        <div className="primary-resource">
          <Typography.Text strong>{auditActionLabel(event.action)}</Typography.Text>
          <small>
            {resourceTypeLabel(event.resource_type)} · {auditTargetLabel(event.resource_type, value, lookups)}
          </small>
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
          { key: 'actor', label: '操作者类型', children: actorTypeLabel(detailEvent.actor_type) },
          { key: 'actor_id', label: '完整操作者标识', children: <Typography.Text code copyable>{detailEvent.actor_id}</Typography.Text> },
          { key: 'action', label: '操作内容', children: auditActionLabel(detailEvent.action) },
          { key: 'resource', label: '操作对象', children: `${resourceTypeLabel(detailEvent.resource_type)} · ${auditTargetLabel(detailEvent.resource_type, detailEvent.resource_id, lookups)}` },
          { key: 'resource_id', label: '完整资源编号', children: <Typography.Text code copyable>{detailEvent.resource_id}</Typography.Text> },
          { key: 'reason', label: '操作原因', children: detailEvent.reason ?? '系统自动操作' },
          { key: 'request', label: '完整请求编号', children: <Typography.Text code copyable>{detailEvent.request_id}</Typography.Text> },
          { key: 'event', label: '完整审计编号', children: <Typography.Text code copyable>{detailEvent.id}</Typography.Text> },
          { key: 'summary', label: '变更详情', children: detailText(detailEvent.summary) },
        ] : []}
      />
    </Space>
  )
}
