import { Button, Card, Select, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useListDeviceHealthEvents, useListDevices } from '../api/generated/device-farm'
import type { Device, HealthEventRecord } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { healthEventTypeLabel, healthReasonLabel, healthSourceLabel, lifecycleStatusLabel, severityLabel } from '../api/labels'
import { detailText, platformLabel } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { PageQueryError, ResourceDetailDrawer, ResourcePageHeader } from '../components/ResourcePage'

const severityColor: Record<string, string> = {
  info: 'default',
  warning: 'orange',
  error: 'red',
  critical: 'magenta',
}

export function HealthEventsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [deviceId, setDeviceId] = useState<string | undefined>(searchParams.get('device_id') ?? undefined)
  const [detailEvent, setDetailEvent] = useState<HealthEventRecord | null>(null)
  const { page, pageSize, onPageChange } = useServerPage(20)
  const devicesQuery = useListDevices({ page: 1, page_size: 200 })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []
  const query = useListDeviceHealthEvents(deviceId ?? '', { page, page_size: pageSize }, {
    query: { enabled: Boolean(deviceId), refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true },
  })
  const result = unwrapPage<HealthEventRecord>(query.data)
  const selectedDevice = devices.find((device) => device.id === deviceId)

  useEffect(() => {
    if (deviceId || devices.length === 0) return
    const preferred = devices.find((device) => device.lifecycle_status === 'quarantined' || device.health_status === 'unhealthy') ?? devices[0]
    setDeviceId(preferred.id)
    setSearchParams({ device_id: preferred.id }, { replace: true })
  }, [deviceId, devices, setSearchParams])

  const columns: TableColumnsType<HealthEventRecord> = [
    { title: '发生时间', dataIndex: 'observed_at', width: 170, render: (value: string) => formatTime(value) },
    { title: '级别', dataIndex: 'severity', width: 100, render: (value: string) => <Tag color={severityColor[value] ?? 'default'}>{severityLabel(value)}</Tag> },
    {
      title: '事件', dataIndex: 'event_type', width: 240, render: (value: string, event) => (
        <div className="primary-resource">
          <Typography.Text strong>{healthEventTypeLabel(value)}</Typography.Text>
          <small>{healthSourceLabel(event.source)}</small>
        </div>
      ),
    },
    { title: '原因说明', dataIndex: 'reason', ellipsis: true, render: (value?: string) => healthReasonLabel(value) },
    { title: '详情', key: 'detail', width: 90, fixed: 'right', render: (_, event) => <Button size="small" onClick={() => setDetailEvent(event)}>查看</Button> },
  ]

  return (
    <Space direction="vertical" size={16} style={{ display: 'flex' }}>
      <ResourcePageHeader
        title="健康事件"
        description="查看指定设备最近出现的健康变化和自动恢复过程。默认优先打开故障设备，日常无需逐台检查健康设备。"
        dataUpdatedAt={query.dataUpdatedAt || devicesQuery.dataUpdatedAt}
        isFetching={query.isFetching || devicesQuery.isFetching}
        onRefresh={() => void Promise.all([devicesQuery.refetch(), deviceId ? query.refetch() : Promise.resolve()])}
        autoRefreshText="每 10 秒自动更新"
      />
      {devicesQuery.isError && <PageQueryError error={devicesQuery.error} onRetry={() => void devicesQuery.refetch()} />}
      {query.isError && <PageQueryError error={query.error} onRetry={() => void query.refetch()} />}
      <Card size="small">
        <Space wrap>
          <Typography.Text>选择设备：</Typography.Text>
          <Select
            showSearch
            style={{ width: 'min(460px, 100%)' }}
            placeholder="选择要查看健康事件的设备"
            optionFilterProp="label"
            value={deviceId}
            onChange={(value) => {
              setDeviceId(value)
              setSearchParams({ device_id: value }, { replace: true })
              onPageChange(1, pageSize)
            }}
            options={devices.map((device) => ({
              value: device.id,
              label: `${platformLabel(device.platform)} · ${shortID(device.id)} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`,
            }))}
            notFoundContent={devicesQuery.isFetching ? '加载中…' : '无设备'}
          />
        </Space>
      </Card>
      <PageTable<HealthEventRecord>
        columns={columns}
        dataSource={deviceId ? result?.items : undefined}
        loading={query.isLoading}
        total={deviceId ? result?.total ?? 0 : 0}
        page={deviceId ? result?.page ?? page : 1}
        pageSize={deviceId ? result?.page_size ?? pageSize : pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: deviceId ? '暂无健康事件' : '请先选择设备' }}
      />
      <ResourceDetailDrawer
        open={detailEvent !== null}
        title={detailEvent ? `健康事件详情 · ${healthEventTypeLabel(detailEvent.event_type)}` : '健康事件详情'}
        onClose={() => setDetailEvent(null)}
        items={detailEvent ? [
          { key: 'device', label: '设备', children: selectedDevice ? `${platformLabel(selectedDevice.platform)} · ${selectedDevice.serial}` : detailEvent.device_id },
          { key: 'observed', label: '发生时间', children: formatTime(detailEvent.observed_at) },
          { key: 'created', label: '记录时间', children: formatTime(detailEvent.created_at) },
          { key: 'severity', label: '级别', children: severityLabel(detailEvent.severity) },
          { key: 'source', label: '来源', children: healthSourceLabel(detailEvent.source) },
          { key: 'type', label: '事件', children: healthEventTypeLabel(detailEvent.event_type) },
          { key: 'reason', label: '原因', children: healthReasonLabel(detailEvent.reason) },
          { key: 'payload', label: '技术详情', children: detailText(detailEvent.payload) },
          { key: 'id', label: '完整事件编号', children: <Typography.Text copyable code>{detailEvent.id}</Typography.Text> },
        ] : []}
      />
    </Space>
  )
}
