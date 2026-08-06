import { App as AntApp, Button, Form, Input, Modal, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
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
  const [form] = Form.useForm<ReasonValues>()
  const [actionState, setActionState] = useState<ActionState | null>(null)

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
    { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '序列号', dataIndex: 'serial', ellipsis: true, render: (value: string) => shortID(value) },
    { title: '类型', dataIndex: 'device_kind', width: 90 },
    { title: '提供方', dataIndex: 'provider_type', width: 130 },
    { title: '生命周期', dataIndex: 'lifecycle_status', width: 110, render: (value: string) => <Tag color={lifecycleColor[value] ?? 'default'}>{value}</Tag> },
    { title: '健康', dataIndex: 'health_status', width: 90, render: (value: string) => <Tag color={healthColor[value] ?? 'default'}>{value}</Tag> },
    { title: '模式', dataIndex: 'lifecycle_mode', width: 110 },
    { title: '宿主机', dataIndex: 'host_id', width: 150, render: (value: string) => shortID(value) },
    { title: 'ADB', dataIndex: 'adb_endpoint', ellipsis: true, render: (value?: string) => value ?? '-' },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    actionColumn,
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDevices({ page, page_size: pageSize })
  const result = unwrapPage<Device>(data)

  return (
    <>
      <PageTable<Device>
        columns={columns}
        dataSource={result?.items}
        loading={isFetching}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
      />
      <Modal
        open={actionState !== null}
        title={actionState ? `${actionTitles[actionState.action]} · ${shortID(actionState.device.id)}` : ''}
        okText="确认执行"
        cancelText="取消"
        confirmLoading={pending}
        onCancel={() => setActionState(null)}
        onOk={() => form.submit()}
        destroyOnClose
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
