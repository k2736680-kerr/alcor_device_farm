import { Card, Select, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { useListDeviceHealthEvents, useListDevices } from '../api/generated/device-farm'
import type { Device, HealthEventRecord } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { healthEventTypeLabel, healthReasonLabel, healthSourceLabel, lifecycleStatusLabel, severityLabel } from '../api/labels'
import { detailText, platformLabel } from '../api/presentation'
import { PageTable } from '../components/PageTable'

const severityColor: Record<string, string> = {
  info: 'default',
  warning: 'orange',
  error: 'red',
  critical: 'magenta',
}

const columns: TableColumnsType<HealthEventRecord> = [
  { title: '时间', dataIndex: 'observed_at', width: 160, render: (value: string) => formatTime(value) },
  { title: '来源', dataIndex: 'source', width: 120, render: (value: string) => <Tag>{healthSourceLabel(value)}</Tag> },
  { title: '事件', dataIndex: 'event_type', width: 160, render: (value: string) => healthEventTypeLabel(value) },
  { title: '级别', dataIndex: 'severity', width: 90, render: (value: string) => <Tag color={severityColor[value] ?? 'default'}>{severityLabel(value)}</Tag> },
  { title: '原因', dataIndex: 'reason', ellipsis: true, render: (value?: string) => healthReasonLabel(value) },
  { title: '事件详情', dataIndex: 'payload', width: 360, ellipsis: true, render: (value: Record<string, unknown>) => detailText(value) },
  { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
]

export function HealthEventsPage() {
  const [deviceId, setDeviceId] = useState<string | undefined>()
  const { page, pageSize, onPageChange } = useServerPage(20)
  const devicesQuery = useListDevices({ page: 1, page_size: 200 })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []
  const { data, isLoading } = useListDeviceHealthEvents(deviceId ?? '', { page, page_size: pageSize }, {
    query: { enabled: Boolean(deviceId), refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true },
  })
  const result = unwrapPage<HealthEventRecord>(data)

  return (
    <Space direction="vertical" size={16} style={{ display: 'flex' }}>
      <Card size="small">
        <Space>
          <Typography.Text>选择设备：</Typography.Text>
          <Select
            showSearch
            style={{ width: 360 }}
            placeholder="选择要查看健康事件的设备"
            optionFilterProp="label"
            value={deviceId}
            onChange={setDeviceId}
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
        loading={isLoading}
        total={deviceId ? result?.total ?? 0 : 0}
        page={deviceId ? result?.page ?? page : 1}
        pageSize={deviceId ? result?.page_size ?? pageSize : pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: deviceId ? '暂无健康事件' : '请先选择设备' }}
      />
    </Space>
  )
}
