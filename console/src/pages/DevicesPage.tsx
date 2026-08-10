import { Alert, App as AntApp, Button, Form, Input, InputNumber, Modal, Segmented, Select, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  getListDevicesQueryKey,
  useDeleteDevice,
  useListDeviceHosts,
  useListDeviceImages,
  useListDevices,
  useQuarantineDevice,
  useRebuildDevice,
  useReimageDevice,
  useRestartDevice,
  useUnquarantineDevice,
} from '../api/generated/device-farm'
import type { ConsoleRole, Device, DeviceHost, DeviceImage, EmulatorRuntimeProfile } from '../api/generated/models'
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

interface ReimageValues extends EmulatorRuntimeProfile {
  image_id: string
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
  const [reimageDevice, setReimageDevice] = useState<Device | null>(null)
  const [reimageForm] = Form.useForm<ReimageValues>()
  const remote = useRemoteControl()
  const view = deviceViewFromQuery(searchParams.get('view'))

  const restart = useRestartDevice()
  const rebuild = useRebuildDevice()
  const reimage = useReimageDevice()
  const quarantine = useQuarantineDevice()
  const unquarantine = useUnquarantineDevice()
  const deleteDevice = useDeleteDevice()
  const imagesQuery = useListDeviceImages({ page: 1, page_size: 200 })
  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 200 })
  const images = unwrapPage<DeviceImage>(imagesQuery.data)?.items ?? []
  const hosts = unwrapPage<DeviceHost>(hostsQuery.data)?.items ?? []
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

  const openReimage = (device: Device) => {
    const profile = device.effective_runtime_profile ?? {}
    setReimageDevice(device)
    reimageForm.setFieldsValue({
      image_id: device.image_id,
      reason: '',
      container_cpu_cores: profile.container_cpu_cores ?? 4,
      container_memory_mb: profile.container_memory_mb ?? 5120,
      guest_cpu_cores: profile.guest_cpu_cores ?? 4,
      guest_memory_mb: profile.guest_memory_mb ?? 4096,
      data_disk_mb: profile.data_disk_mb ?? 4096,
      image_disk_mb: profile.image_disk_mb ?? 0,
      width: profile.width ?? 1080,
      height: profile.height ?? 2400,
      density_dpi: profile.density_dpi ?? 420,
      vm_heap_mb: profile.vm_heap_mb ?? 512,
      graphics: profile.graphics ?? 'auto',
    })
  }

  const submitReimage = (values: ReimageValues) => {
    if (!reimageDevice) return
    const { image_id, reason, ...runtime_profile } = values
    modal.confirm({
      title: '确认更换镜像并重装？',
      content: '当前模拟器会被删除并重新创建，已上传的 APK、应用数据、缓存和设备文件都会清空。目标启动失败时系统只尝试恢复一次旧配置。',
      okText: '确认清空并重装',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => reimage.mutate({ id: reimageDevice.id, data: { image_id, runtime_profile, reason: reason.trim() } }, {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`重装任务已受理（request_id: ${requestID}）`)
          setReimageDevice(null)
          invalidate()
        },
        onError: (error) => {
          const err = error as { code?: string; requestId?: string; message?: string }
          message.error(`重装被拒绝（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
        },
      }),
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
        {role === 'admin' && device.device_kind === 'emulator' && device.provider_type === 'docker_emulator'
          && ['ready', 'stopped', 'quarantined'].includes(device.lifecycle_status) && device.reimage_status !== 'pending' && (
          <Button size="small" onClick={() => openReimage(device)}>编辑配置</Button>
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
    { title: '配置状态', dataIndex: 'reimage_status', width: 130, render: (value: string, device) => value === 'pending'
      ? <Tag color="processing">正在换镜像</Tag>
      : value === 'failed' ? <Tag color="red" title={device.reimage_error}>上次重装失败</Tag> : <Tag>已生效</Tag> },
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
      <Modal
        open={reimageDevice !== null}
        title={reimageDevice ? `编辑配置/更换镜像 · ${shortID(reimageDevice.id)}` : ''}
        okText="下一步"
        cancelText="取消"
        confirmLoading={reimage.isPending}
        onCancel={() => setReimageDevice(null)}
        onOk={() => reimageForm.submit()}
        width={720}
        destroyOnHidden
      >
        <Alert type="warning" showIcon message="重装会清空这台模拟器里的 APK 和全部设备数据" style={{ marginBottom: 16 }} />
        <Typography.Paragraph type="secondary">
          只允许没有预约、没有其他处理中操作的空闲设备修改。提交时服务端会按宿主机最新 CPU、内存和磁盘重新计算；空间不足会直接拒绝，不会先删除旧设备。
        </Typography.Paragraph>
        <Form<ReimageValues> form={reimageForm} layout="vertical" onFinish={submitReimage}>
          <Form.Item name="image_id" label="系统镜像" rules={[{ required: true, message: '请选择已验证镜像' }]}>
            <Select options={images.filter((image) => image.status === 'ready' && image.docker_image).map((image) => ({
              value: image.id, label: `${image.name} · Android API ${image.api_level} · ${image.abi}`,
            }))} onChange={(imageID) => {
              const image = images.find((item) => item.id === imageID)
              if (image?.resource_config) reimageForm.setFieldsValue(image.resource_config)
            }} />
          </Form.Item>
          <Space wrap align="start">
            <Form.Item name="container_cpu_cores" label="容器 CPU 核数" rules={[{ required: true }]}><InputNumber min={1} max={64} step={0.5} /></Form.Item>
            <Form.Item name="container_memory_mb" label="容器内存 MB" rules={[{ required: true }]}><InputNumber min={2048} max={262144} step={512} /></Form.Item>
            <Form.Item name="guest_cpu_cores" label="Android CPU 核数" rules={[{ required: true }]}><InputNumber min={1} max={32} /></Form.Item>
            <Form.Item name="guest_memory_mb" label="Android 内存 MB" rules={[{ required: true }]}><InputNumber min={1536} max={261632} step={512} /></Form.Item>
            <Form.Item name="data_disk_mb" label="设备数据盘 MB" rules={[{ required: true }]}><InputNumber min={2048} max={1048576} step={1024} /></Form.Item>
            <Form.Item name="graphics" label="图形加速" rules={[{ required: true }]}><Select style={{ width: 130 }} options={[
              { value: 'auto', label: '自动' }, { value: 'host', label: '宿主机 GPU' }, { value: 'software', label: '软件渲染' },
            ]} /></Form.Item>
          </Space>
          <Typography.Paragraph type="secondary">
            当前宿主机：{hosts.find((host) => host.id === reimageDevice?.host_id)?.name ?? shortID(reimageDevice?.host_id ?? '')}。页面显示的是配置值，最终容量以提交瞬间服务端重新计算为准。
          </Typography.Paragraph>
          <Form.Item name="reason" label="修改原因（必填，将写入审计）" rules={[
            { required: true, whitespace: true, message: '请填写修改原因' }, { min: 3, message: '修改原因至少填写 3 个字' },
          ]}>
            <Input.TextArea rows={3} maxLength={200} placeholder="例如：需要验证 Android 15 兼容性" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
