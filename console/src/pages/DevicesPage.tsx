import { Alert, App as AntApp, Button, Form, Input, Modal, Segmented, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  getListDevicesQueryKey,
  useDeleteDevice,
  useListDevices,
  useQuarantineDevice,
  useRebuildDevice,
  useRestartDevice,
  useUnquarantineDevice,
} from '../api/generated/device-farm'
import type { ConsoleRole, Device } from '../api/generated/models'
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
import { useRemoteControl } from '../remote/RemoteControlProvider'

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

type DeviceAction = 'restart' | 'rebuild' | 'quarantine' | 'unquarantine' | 'delete'
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

interface DevicesPageProps {
  role?: ConsoleRole
}

const actionTitles: Record<DeviceAction, string> = {
  restart: '重启设备',
  rebuild: '重建设备',
  quarantine: '隔离设备',
  unquarantine: '解除隔离',
  delete: '删除设备',
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
    case 'delete':
      return device.lifecycle_status === 'quarantined' || device.lifecycle_status === 'stopped'
    default:
      return true
  }
}

export function DevicesPage({ role = 'admin' }: DevicesPageProps) {
  const { message, modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<ReasonValues>()
  const [actionState, setActionState] = useState<ActionState | null>(null)
  const remote = useRemoteControl()
  const view = deviceViewFromQuery(searchParams.get('view'))

  const restart = useRestartDevice()
  const rebuild = useRebuildDevice()
  const quarantine = useQuarantineDevice()
  const unquarantine = useUnquarantineDevice()
  const deleteDevice = useDeleteDevice()
  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
  }, [queryClient])

  const executeAction = (reason: string) => {
    if (!actionState) {
      return
    }
    const { device, action } = actionState
    const mutation =
      action === 'restart' ? restart
      : action === 'rebuild' ? rebuild
      : action === 'quarantine' ? quarantine
      : action === 'unquarantine' ? unquarantine
      : deleteDevice

    mutation.mutate(
      { id: device.id, data: { reason } },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`${action === 'delete' ? '删除任务' : '操作'}已受理（request_id: ${requestID}）`)
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

  const submitAction = (values: ReasonValues) => {
    if (!actionState) {
      return
    }
    const reason = values.reason.trim()
    if (actionState.action !== 'delete') {
      executeAction(reason)
      return
    }
    modal.confirm({
      title: '确认删除这台设备？',
      content: '系统将通过宿主代理清理容器、网络和数据卷，并把设备转入已删除历史。设备池目标数量不变时，系统可能自动补建一台。',
      okText: '确认删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => executeAction(reason),
    })
  }

  const pending = restart.isPending || rebuild.isPending || quarantine.isPending || unquarantine.isPending || deleteDevice.isPending

  const actionColumn: TableColumnsType<Device>[number] = {
    title: '操作',
    key: 'actions',
    width: 340,
    fixed: 'right',
    render: (_, device) => (
      <Space size={4} wrap>
        {role === 'admin' && device.lifecycle_status === 'ready' && device.health_status === 'healthy' && (
          <Button
            type="primary"
            size="small"
            loading={remote.isStarting && remote.device?.id === device.id}
            disabled={remote.device !== null && remote.device.id !== device.id}
            onClick={() => remote.start(device)}
          >远程连接</Button>
        )}
        {role === 'admin' && remote.device?.id === device.id && (
          <Button size="small" danger loading={remote.isEnding} onClick={() => remote.end(true)}>
            {remote.view?.status === 'connected' ? '挂断' : '取消连接'}
          </Button>
        )}
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
        {actionable(device, 'delete') && (
          <Button size="small" danger onClick={() => setActionState({ device, action: 'delete' })}>删除</Button>
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
  const deviceQueryOptions = { query: { refetchInterval: 5_000, refetchOnWindowFocus: true, refetchOnReconnect: true } }
  const availableCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'ready', health_status: 'healthy' }, deviceQueryOptions)
  const busyCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'busy' }, deviceQueryOptions)
  const quarantinedCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'quarantined' }, deviceQueryOptions)
  const deletedCountQuery = useListDevices({ page: 1, page_size: 1, lifecycle_status: 'deleted' }, deviceQueryOptions)
  const allCountQuery = useListDevices({ page: 1, page_size: 1 }, deviceQueryOptions)
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
  const { data, isLoading } = useListDevices({ page, page_size: pageSize, ...viewFilter }, deviceQueryOptions)
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
          loading={isLoading}
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
        okText={actionState?.action === 'delete' ? '下一步' : '确认执行'}
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
          {actionState?.action === 'delete' && '删除只允许隔离或已停止且没有活动预约的设备。成功后会清理运行资源并转入已删除历史；目标数量不变时系统可能自动补建。'}
        </Typography.Paragraph>
        <Form<ReasonValues> form={form} layout="vertical" onFinish={submitAction}>
          <Form.Item name="reason" label="操作原因（必填，将写入审计）" rules={[
            { required: true, whitespace: true, message: '请填写操作原因' },
            { min: 3, message: '操作原因至少填写 3 个字' },
          ]}>
            <Input.TextArea rows={3} maxLength={200} placeholder={actionState?.action === 'delete' ? '例如：设备无法恢复，确认清理运行资源' : '例如：镜像异常，需要重建验证'} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
