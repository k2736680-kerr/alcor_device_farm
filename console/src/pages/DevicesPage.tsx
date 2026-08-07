import { Alert, App as AntApp, Button, Form, Input, Modal, Segmented, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  getListDevicesQueryKey,
  useListDevices,
  useQuarantineDevice,
  useRebuildDevice,
  useRestartDevice,
  useUnquarantineDevice,
} from '../api/generated/device-farm'
import type { Device } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import {
  deviceKindLabel,
  healthReasonLabel,
  healthStatusLabel,
  lifecycleModeLabel,
  lifecycleStatusLabel,
  providerTypeLabel,
} from '../api/labels'
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

type DeviceAction = 'restart' | 'rebuild' | 'quarantine' | 'unquarantine'
type DeviceView = 'available' | 'busy' | 'quarantined' | 'deleted' | 'all'

const deviceViews: DeviceView[] = ['available', 'busy', 'quarantined', 'deleted', 'all']

function deviceViewFromQuery(value: string | null): DeviceView {
  return deviceViews.includes(value as DeviceView) ? value as DeviceView : 'available'
}

interface ActionState {
  device: Device
  action: DeviceAction
}

interface ReasonValues {
  reason: string
}

const actionTitles: Record<DeviceAction, string> = {
  restart: '重启设备',
  rebuild: '重建设备',
  quarantine: '隔离设备',
  unquarantine: '解除隔离',
}

function actionable(device: Device, action: DeviceAction): boolean {
  if (device.lifecycle_status === 'deleted') {
    return false
  }
  switch (action) {
    case 'quarantine':
      return device.lifecycle_status !== 'quarantined'
    case 'unquarantine':
      return device.lifecycle_status === 'quarantined'
    default:
      return true
  }
}

export function DevicesPage() {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<ReasonValues>()
  const [actionState, setActionState] = useState<ActionState | null>(null)
  const view = deviceViewFromQuery(searchParams.get('view'))

  const restart = useRestartDevice()
  const rebuild = useRebuildDevice()
  const quarantine = useQuarantineDevice()
  const unquarantine = useUnquarantineDevice()

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
  }

  const submitAction = (values: ReasonValues) => {
    if (!actionState) {
      return
    }
    const { device, action } = actionState
    const mutation =
      action === 'restart' ? restart
      : action === 'rebuild' ? rebuild
      : action === 'quarantine' ? quarantine
      : unquarantine

    mutation.mutate(
      { id: device.id, data: { reason: values.reason } },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`操作已受理（request_id: ${requestID}）`)
          setActionState(null)
          invalidate()
        },
        onError: (error) => {
          const err = error as { code?: string; requestId?: string; message?: string }
          message.error(`操作被拒绝（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
        },
      },
    )
  }

  const pending = restart.isPending || rebuild.isPending || quarantine.isPending || unquarantine.isPending

  const actionColumn: TableColumnsType<Device>[number] = {
    title: '操作',
    key: 'actions',
    width: 260,
    fixed: 'right',
    render: (_, device) => (
      <Space size={4} wrap>
        {actionable(device, 'restart') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'restart' })}>重启</Button>
        )}
        {actionable(device, 'rebuild') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'rebuild' })}>重建</Button>
        )}
        {actionable(device, 'quarantine') && (
          <Button size="small" danger onClick={() => setActionState({ device, action: 'quarantine' })}>隔离</Button>
        )}
        {actionable(device, 'unquarantine') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'unquarantine' })}>解除隔离</Button>
        )}
      </Space>
    ),
  }

  const columns: TableColumnsType<Device> = [
    { title: '设备编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '设备标识', dataIndex: 'serial', width: 170, ellipsis: true },
    { title: '设备类型', dataIndex: 'device_kind', width: 120, render: (value: string) => deviceKindLabel(value) },
    { title: '运行方式', dataIndex: 'provider_type', width: 130, render: (value: string) => providerTypeLabel(value) },
    { title: '设备状态', dataIndex: 'lifecycle_status', width: 110, render: (value: string) => <Tag color={lifecycleColor[value] ?? 'default'}>{lifecycleStatusLabel(value)}</Tag> },
    { title: '健康状态', dataIndex: 'health_status', width: 110, render: (value: string) => <Tag color={healthColor[value] ?? 'default'}>{healthStatusLabel(value)}</Tag> },
    { title: '清理方式', dataIndex: 'lifecycle_mode', width: 110, render: (value: string) => lifecycleModeLabel(value) },
    { title: '所属宿主机', dataIndex: 'host_id', width: 150, render: (value: string) => shortID(value) },
    { title: 'ADB 地址', dataIndex: 'adb_endpoint', width: 170, ellipsis: true, render: (value?: string) => value ?? '-' },
    { title: '状态说明', dataIndex: 'health_reason', width: 220, ellipsis: true, render: (value?: string) => healthReasonLabel(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    actionColumn,
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const availableCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'ready', health_status: 'healthy' })
  const busyCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'busy' })
  const quarantinedCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'quarantined' })
  const deletedCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'deleted' })
  const allCountQuery = useListDevices({ page: 1, page_size: 1 })
  const availableCount = unwrapPage<Device>(availableCountQuery.data)?.total ?? 0
  const busyCount = unwrapPage<Device>(busyCountQuery.data)?.total ?? 0
  const quarantinedCount = unwrapPage<Device>(quarantinedCountQuery.data)?.total ?? 0
  const deletedCount = unwrapPage<Device>(deletedCountQuery.data)?.total ?? 0
  const allCount = unwrapPage<Device>(allCountQuery.data)?.total ?? 0
  const viewFilter =
    view === 'available' ? { lifecycle_status: 'ready' as const, health_status: 'healthy' as const }
    : view === 'busy' ? { lifecycle_status: 'busy' as const }
    : view === 'quarantined' ? { lifecycle_status: 'quarantined' as const }
    : view === 'deleted' ? { lifecycle_status: 'deleted' as const }
    : {}
  const { data, isFetching } = useListDevices({ page, page_size: pageSize, ...viewFilter })
  const result = unwrapPage<Device>(data)

  return (
    <>
      <Space direction="vertical" size={14} style={{ display: 'flex' }}>
        <Alert
          type="info"
          showIcon
          message="默认只显示当前可以预约的设备"
          description="隔离设备用于排查故障，已删除设备只保留历史记录；它们都不会计入可用设备数量。"
        />
        <Segmented<DeviceView>
          value={view}
          options={[
            { label: `可用设备（${availableCount}）`, value: 'available' },
            { label: `使用中（${busyCount}）`, value: 'busy' },
            { label: `隔离设备（${quarantinedCount}）`, value: 'quarantined' },
            { label: `已删除历史（${deletedCount}）`, value: 'deleted' },
            { label: `全部记录（${allCount}）`, value: 'all' },
          ]}
          onChange={(nextView) => {
            const nextSearchParams = new URLSearchParams(searchParams)
            if (nextView === 'available') {
              nextSearchParams.delete('view')
            } else {
              nextSearchParams.set('view', nextView)
            }
            setSearchParams(nextSearchParams, { replace: true })
            onPageChange(1, pageSize)
          }}
        />
        <PageTable<Device>
          columns={columns}
          dataSource={result?.items}
          loading={isFetching}
          total={result?.total ?? 0}
          page={result?.page ?? page}
          pageSize={result?.page_size ?? pageSize}
          onPageChange={onPageChange}
          locale={{ emptyText: '当前分类下没有设备' }}
        />
      </Space>
      <Modal
        open={actionState !== null}
        title={actionState ? `${actionTitles[actionState.action]} · ${shortID(actionState.device.id)}` : ''}
        okText="确认执行"
        cancelText="取消"
        confirmLoading={pending}
        onCancel={() => setActionState(null)}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary">
          {actionState?.action === 'quarantine' && '隔离后设备将不再接受新预约，已激活会话不受影响。'}
          {actionState?.action === 'unquarantine' && '解除隔离后设备可重新进入调度池。'}
          {actionState?.action === 'rebuild' && '重建会销毁并重新拉起设备运行实例，属于危险操作。'}
          {actionState?.action === 'restart' && '重启会中断当前设备上的会话。'}
        </Typography.Paragraph>
        <Form<ReasonValues> form={form} layout="vertical" onFinish={submitAction}>
          <Form.Item name="reason" label="操作原因（必填，将写入审计）" rules={[{ required: true, whitespace: true, message: '请填写操作原因' }]}>
            <Input.TextArea rows={3} maxLength={200} placeholder="例如：镜像异常，需要重建验证" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
