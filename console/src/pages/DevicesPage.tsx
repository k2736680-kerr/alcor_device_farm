import { Alert, App as AntApp, Button, Card, Collapse, Form, Input, InputNumber, Modal, Segmented, Select, Space, Steps, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  getListDevicesQueryKey,
  getListDevicePoolsQueryKey,
  useCreateDeviceProvisioning,
  useCreateIOSSimulator,
  useDeleteDevice,
  useGetIOSSimulatorCatalog,
  useListAndroidHardwareProfiles,
  useListAndroidSystemImages,
  useGetDeviceProvisioning,
  useListDeviceHosts,
  useListDeviceImages,
  useListDevicePools,
  useListDevices,
  useQuarantineDevice,
  useRebuildDevice,
  useReimageDevice,
  useRestartDevice,
  useStartDevice,
  useStopDevice,
  useUnquarantineDevice,
} from '../api/generated/device-farm'
import type { AndroidHardwareProfile, AndroidSystemImage, ConsoleRole, Device, DeviceHost, DeviceImage, DevicePool, EmulatorRuntimeProfile, IOSSimulatorCatalog } from '../api/generated/models'
import { unwrapData, unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { androidVersionLabel, formatTime, shortID } from '../api/format'
import {
  deviceKindLabel,
  healthReasonLabel,
  healthStatusLabel,
  lifecycleModeLabel,
  lifecycleStatusLabel,
  providerTypeLabel,
} from '../api/labels'
import { apiErrorText, platformLabel, responseRequestID } from '../api/presentation'
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

type DeviceAction = 'start' | 'stop' | 'restart' | 'rebuild' | 'quarantine' | 'unquarantine' | 'delete'
type DeviceView = 'available' | 'busy' | 'quarantined' | 'deleted' | 'all'
type PlatformView = 'all' | 'android' | 'ios'

const deviceViews: DeviceView[] = ['available', 'busy', 'quarantined', 'deleted', 'all']

function deviceViewFromQuery(value: string | null): DeviceView {
  return deviceViews.includes(value as DeviceView) ? value as DeviceView : 'available'
}

function platformFromQuery(value: string | null): PlatformView {
  return value === 'android' || value === 'ios' ? value : 'all'
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

interface CreateDeviceValues extends EmulatorRuntimeProfile {
  pool_id: string
  catalog_id: string
  hardware_profile_id: string
}

interface CreateIOSSimulatorValues {
  host_id: string
  pool_id: string
  runtime_id: string
  device_type_id: string
  display_name?: string
  reason: string
}

interface DevicesPageProps {
  role?: ConsoleRole
}

const actionTitles: Record<DeviceAction, string> = {
  start: '启动设备',
  stop: '停止设备',
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
    case 'start':
      return device.platform === 'ios' && device.lifecycle_status === 'stopped'
    case 'stop':
      return device.platform === 'ios' && device.lifecycle_status === 'ready'
    case 'restart':
      return device.platform === 'android'
    case 'rebuild':
      return (device.platform === 'android' && device.device_kind === 'emulator' && device.provider_type === 'docker_emulator')
        || (device.platform === 'ios' && device.device_kind === 'simulator' && device.provider_type === 'appium_device_farm_ios')
    case 'quarantine':
      return device.lifecycle_status !== 'quarantined'
    case 'unquarantine':
      return device.lifecycle_status === 'quarantined'
    case 'delete':
      return device.lifecycle_status === 'ready' || device.lifecycle_status === 'quarantined' || device.lifecycle_status === 'stopped'
    default:
      return true
  }
}

function iosSystemVersion(device: Device): string {
  const platformVersion = device.capabilities.platformVersion
  if (typeof platformVersion === 'string' && platformVersion.trim()) return `iOS ${platformVersion}`
  const runtime = device.capabilities.runtimeId
  if (typeof runtime === 'string') {
    const marker = runtime.match(/iOS[-.]([0-9-]+)$/i)?.[1]
    if (marker) return `iOS ${marker.replaceAll('-', '.')}`
  }
  return 'iOS（版本待上报）'
}

function androidCatalogStatus(value: string): string {
  const labels: Record<string, string> = {
    downloadable: '可下载', preparing: '下载或构建中', validating: '验证中', cached: '已缓存可用',
    failed: '准备失败', official_updated: '官方版本已更新',
  }
  return labels[value] ?? '未知状态'
}

function androidImageType(value: string): string {
  if (value === 'google_play') return 'Google Play'
  if (value === 'google_apis') return 'Google APIs'
  return '其他镜像类型'
}

function androidCatalogOptionLabel(image: AndroidSystemImage): string {
  return `Android ${image.api_level - 20} / API ${image.api_level} · ${androidImageType(image.image_type)} · ${image.abi} · ${androidCatalogStatus(image.status)}`
}

function defaultAndroidCatalogID(catalog: AndroidSystemImage[]): string | undefined {
  return catalog.find((image) => image.api_level === 36 && image.image_type === 'google_apis' && image.abi === 'x86_64')?.id
    ?? catalog.find((image) => image.status === 'cached')?.id
    ?? catalog[0]?.id
}

export function DevicesPage({ role = 'admin' }: DevicesPageProps) {
  const { message, modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<ReasonValues>()
  const [actionState, setActionState] = useState<ActionState | null>(null)
  const [reimageDevice, setReimageDevice] = useState<Device | null>(null)
  const [reimageForm] = Form.useForm<ReimageValues>()
  const [createDevice, setCreateDevice] = useState(false)
  const [createForm] = Form.useForm<CreateDeviceValues>()
  const [createStep, setCreateStep] = useState(0)
  const [profileSearch, setProfileSearch] = useState('')
  const [provisioningID, setProvisioningID] = useState<string | null>(null)
  const [createIOS, setCreateIOS] = useState(false)
  const [iosCreateStep, setIOSCreateStep] = useState(0)
  const [iosForm] = Form.useForm<CreateIOSSimulatorValues>()
  // 创建向导切换步骤会卸载第一步的表单项；保留已选 Mac，避免第二步停止读取其目录。
  const selectedIOSHostID = Form.useWatch('host_id', { form: iosForm, preserve: true })
  const selectedIOSRuntimeID = Form.useWatch('runtime_id', { form: iosForm, preserve: true })
  const remote = useRemoteControl()
  const view = deviceViewFromQuery(searchParams.get('view'))
  const platformView = platformFromQuery(searchParams.get('platform'))

  const start = useStartDevice()
  const stop = useStopDevice()
  const restart = useRestartDevice()
  const rebuild = useRebuildDevice()
  const reimage = useReimageDevice()
  const provision = useCreateDeviceProvisioning()
  const quarantine = useQuarantineDevice()
  const unquarantine = useUnquarantineDevice()
  const deleteDevice = useDeleteDevice()
  const createIOSSimulator = useCreateIOSSimulator()
  const imagesQuery = useListDeviceImages({ page: 1, page_size: 200, status: 'ready' })
  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 200 })
  const images = unwrapPage<DeviceImage>(imagesQuery.data)?.items ?? []
  const hosts = unwrapPage<DeviceHost>(hostsQuery.data)?.items ?? []
  const hardwareQuery = useListAndroidHardwareProfiles()
  const catalogQuery = useListAndroidSystemImages()
  const poolsQuery = useListDevicePools({ page: 1, page_size: 200 })
  const hardwareProfiles = unwrapData<AndroidHardwareProfile[]>(hardwareQuery.data) ?? []
  const catalog = unwrapData<AndroidSystemImage[]>(catalogQuery.data) ?? []
  const pools = unwrapPage<DevicePool>(poolsQuery.data)?.items ?? []
  const androidPools = pools.filter((pool) => pool.platform === 'android' && pool.status === 'active')
  const iosHosts = hosts.filter((host) => host.host_os === 'macos' && host.status === 'online' && !host.draining)
  const iosPools = pools.filter((pool) => pool.platform === 'ios' && pool.status === 'active')
  const iosCatalogQuery = useGetIOSSimulatorCatalog(
    { host_id: selectedIOSHostID ?? '' },
    { query: { enabled: Boolean(selectedIOSHostID), retry: false } },
  )
  const iosCatalog = unwrapData<IOSSimulatorCatalog>(iosCatalogQuery.data)
  const compatibleIOSDeviceTypes = useMemo(() => {
    const supported = new Set(iosCatalog?.runtimes.find((runtime) => runtime.id === selectedIOSRuntimeID)?.device_type_ids ?? [])
    return (iosCatalog?.device_types ?? []).filter((deviceType) => supported.has(deviceType.id))
  }, [iosCatalog, selectedIOSRuntimeID])
  const defaultIOSHostID = iosHosts[0]?.id
  const defaultIOSPoolID = iosPools[0]?.id
  const imageByID = useMemo(() => new Map(images.map((image) => [image.id, image])), [images])
  const poolByID = useMemo(() => new Map(pools.map((pool) => [pool.id, pool])), [pools])
  const filteredHardwareProfiles = useMemo(() => {
    const needle = profileSearch.trim().toLowerCase()
    return needle === '' ? hardwareProfiles : hardwareProfiles.filter((profile) => profile.name.toLowerCase().includes(needle) || profile.id.includes(needle))
  }, [hardwareProfiles, profileSearch])
  const provisioningQuery = useGetDeviceProvisioning(provisioningID ?? '', {
    query: { enabled: provisioningID !== null, refetchInterval: provisioningID ? 2_000 : false },
  })
  const provisioningState = unwrapData<{
    id: string
    status: string
    error_stage?: string
    error_code?: string
    capacity_result?: { limiting_resource?: string; shortfall?: Record<string, number> }
  }>(provisioningQuery.data)
  const provisioningRequestID = responseRequestID(provisioningQuery.data)

  const capacityMessage = useMemo(() => {
    const result = provisioningState?.capacity_result
    if (!result) return '当前没有满足条件且容量充足的宿主机，请检查宿主机在线状态和资源上报。'
    const shortfall = result.shortfall ?? {}
    const parts: string[] = []
    if ((shortfall.memory_mb ?? 0) > 0) parts.push(`内存还缺 ${shortfall.memory_mb} MB`)
    if ((shortfall.disk_mb ?? 0) > 0) parts.push(`磁盘还缺 ${shortfall.disk_mb} MB`)
    if ((shortfall.cpu_millicores ?? 0) > 0) parts.push(`CPU 还缺 ${(shortfall.cpu_millicores / 1000).toFixed(3)} 核`)
    if ((shortfall.device_slots ?? 0) > 0) parts.push(`设备名额还缺 ${shortfall.device_slots} 个`)
    return parts.length > 0 ? `宿主机资源不足：${parts.join('，')}。容量恢复后会自动继续创建。` : '当前没有满足条件且容量充足的宿主机，请检查宿主机在线状态和资源上报。'
  }, [provisioningState])
  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
  }, [queryClient])

  const executeAction = (reason: string) => {
    if (!actionState) {
      return
    }
    const { device, action } = actionState
    const mutation =
      action === 'start' ? start
      : action === 'stop' ? stop
      : action === 'restart' ? restart
      : action === 'rebuild' ? rebuild
      : action === 'quarantine' ? quarantine
      : action === 'unquarantine' ? unquarantine
      : deleteDevice

    mutation.mutate(
      { id: device.id, data: { reason } },
      {
        onSuccess: (data) => {
          message.success(`${action === 'delete' ? '删除任务' : '设备操作'}已受理（请求编号：${responseRequestID(data)}）`)
          setActionState(null)
          invalidate()
        },
        onError: (error) => {
          message.error(`操作被拒绝：${apiErrorText(error)}`)
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
      content: actionState.device.platform === 'ios'
        ? '系统将通过 Mac 宿主代理关闭并删除 CoreSimulator 虚拟 iPhone，同时把设备转入已删除历史并减少设备池目标数量。'
        : '系统将通过宿主代理清理容器、网络和数据卷，并把设备转入已删除历史，同时把所属设备池的目标数量减少一台，不会自动补建。',
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
          message.success(`重装任务已受理（请求编号：${responseRequestID(data)}）`)
          setReimageDevice(null)
          invalidate()
        },
        onError: (error) => {
          message.error(`重装被拒绝：${apiErrorText(error)}`)
        },
      }),
    })
  }

  const openCreateDevice = () => {
    createForm.setFieldsValue({
      pool_id: androidPools[0]?.id,
      hardware_profile_id: 'pixel_9',
      catalog_id: defaultAndroidCatalogID(catalog),
      container_cpu_cores: 4, container_memory_mb: 5120, guest_cpu_cores: 4, guest_memory_mb: 4096,
      data_disk_mb: 4096, image_disk_mb: 0, width: 1080, height: 2424, density_dpi: 420, vm_heap_mb: 512, graphics: 'auto',
    })
    setCreateStep(0)
    setProfileSearch('')
    setCreateDevice(true)
  }

  const submitCreateDevice = (values: CreateDeviceValues) => {
    const { pool_id, catalog_id, hardware_profile_id, ...runtime_profile } = values
    provision.mutate({ data: { pool_id, catalog_id, hardware_profile_id, runtime_profile } }, {
      onSuccess: (data) => {
        const response = data as { request_id?: string; data?: { id?: string } }
        const jobID = response.data?.id
        if (jobID) setProvisioningID(jobID)
        message.success(`Android 模拟器创建流程已受理（请求编号：${responseRequestID(data)}）`)
        setCreateDevice(false)
      },
      onError: (error) => {
        message.error(`Android 模拟器创建被拒绝：${apiErrorText(error)}`)
      },
    })
  }

  const openCreateIOS = () => {
    iosForm.resetFields()
    setIOSCreateStep(0)
    setCreateIOS(true)
  }

  const submitCreateIOS = (values: CreateIOSSimulatorValues) => {
    createIOSSimulator.mutate({ data: {
      host_id: values.host_id,
      pool_id: values.pool_id,
      runtime_id: values.runtime_id,
      device_type_id: values.device_type_id,
      display_name: values.display_name?.trim() || undefined,
      reason: values.reason.trim(),
    } }, {
      onSuccess: (data) => {
        message.success(`iOS 模拟器创建任务已受理（请求编号：${responseRequestID(data)}）`)
        setCreateIOS(false)
        iosForm.resetFields()
        void queryClient.invalidateQueries({ queryKey: getListDevicePoolsQueryKey() })
        invalidate()
      },
      onError: (error) => {
        message.error(`iOS 模拟器创建被拒绝：${apiErrorText(error)}`)
      },
    })
  }


  useEffect(() => {
    if (!provisioningState) return
    if (provisioningState.status === 'ready') {
	  message.destroy('device-provision-capacity')
      message.success('Android 模拟器已通过 ADB、STF 和 Appium 检查，可以使用')
      setProvisioningID(null)
      invalidate()
    }
    if (provisioningState.status === 'failed') {
	  message.destroy('device-provision-capacity')
	  message.error(`设备创建失败（错误代码：${provisioningState.error_code ?? '未知'}；请求编号：${provisioningRequestID}），请联系管理员检查宿主机创建日志`)
	  setProvisioningID(null)
    }
    if (provisioningState.status === 'waiting_capacity') {
	  message.warning({ key: 'device-provision-capacity', content: capacityMessage, duration: 0 })
    }
  }, [capacityMessage, invalidate, message, provisioningRequestID, provisioningState])

  const initializeIOSCreateForm = (open: boolean) => {
    if (!open) return
    iosForm.setFieldsValue({
      host_id: iosForm.getFieldValue('host_id') || defaultIOSHostID,
      pool_id: iosForm.getFieldValue('pool_id') || defaultIOSPoolID,
      display_name: iosForm.getFieldValue('display_name') ?? '',
      reason: iosForm.getFieldValue('reason') ?? '',
    })
  }

  useEffect(() => {
    if (!createIOS || !defaultIOSHostID) return
    initializeIOSCreateForm(true)
  }, [createIOS, defaultIOSHostID, defaultIOSPoolID])

  useEffect(() => {
    if (!createDevice || createForm.getFieldValue('catalog_id') || catalog.length === 0) return
    createForm.setFieldValue('catalog_id', defaultAndroidCatalogID(catalog))
  }, [catalog, createDevice, createForm])

  useEffect(() => {
    if (!createIOS || !iosCatalog || iosCatalog.runtimes.length === 0) return
    const currentRuntimeID = iosForm.getFieldValue('runtime_id')
    const runtime = iosCatalog.runtimes.find((item) => item.id === currentRuntimeID) ?? iosCatalog.runtimes[0]
    const supportedTypes = new Set(runtime.device_type_ids ?? [])
    const currentDeviceTypeID = iosForm.getFieldValue('device_type_id')
    const deviceType = iosCatalog.device_types.find((item) => item.id === currentDeviceTypeID && supportedTypes.has(item.id))
      ?? iosCatalog.device_types.find((item) => supportedTypes.has(item.id))
    iosForm.setFieldsValue({ runtime_id: runtime.id, device_type_id: deviceType?.id })
  }, [createIOS, iosCatalog, iosForm])

  const pending = start.isPending || stop.isPending || restart.isPending || rebuild.isPending || quarantine.isPending || unquarantine.isPending || deleteDevice.isPending

  const actionColumn: TableColumnsType<Device>[number] = {
    title: '操作',
    key: 'actions',
    width: 340,
    fixed: 'right',
    render: (_, device) => (
      <Space size={4} wrap>
        {role === 'viewer' && <Typography.Text type="secondary">只读</Typography.Text>}
        {role === 'admin' && (device.platform === 'android' || (device.platform === 'ios' && device.device_kind === 'simulator'))
          && device.lifecycle_status === 'ready' && device.health_status === 'healthy' && (
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
        {role !== 'viewer' && actionable(device, 'start') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'start' })}>启动</Button>
        )}
        {role !== 'viewer' && actionable(device, 'stop') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'stop' })}>停止</Button>
        )}
        {role !== 'viewer' && actionable(device, 'restart') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'restart' })}>重启</Button>
        )}
        {role === 'admin' && actionable(device, 'rebuild') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'rebuild' })}>重建</Button>
        )}
        {role === 'admin' && actionable(device, 'quarantine') && (
          <Button size="small" danger onClick={() => setActionState({ device, action: 'quarantine' })}>隔离</Button>
        )}
        {role === 'admin' && actionable(device, 'unquarantine') && (
          <Button size="small" onClick={() => setActionState({ device, action: 'unquarantine' })}>解除隔离</Button>
        )}
        {role === 'admin' && actionable(device, 'delete') && (
          <Button size="small" danger onClick={() => setActionState({ device, action: 'delete' })}>删除</Button>
        )}
      </Space>
    ),
  }

  const columns: TableColumnsType<Device> = [
    { title: '设备编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '平台', dataIndex: 'platform', width: 90, render: (value: string) => <Tag color={value === 'ios' ? 'blue' : 'green'}>{platformLabel(value)}</Tag> },
    { title: '设备型号', width: 170, render: (_, device) => String(device.platform === 'ios' ? (device.capabilities.model ?? device.capabilities.deviceName ?? '-') : (device.capabilities.hardware_profile_name ?? device.capabilities.hardware_profile_id ?? '-')) },
    {
      title: '系统版本', width: 190, render: (_, device) => {
        if (device.platform === 'ios') {
          return <Typography.Text>{iosSystemVersion(device)}</Typography.Text>
        }
        const image = device.image_id ? imageByID.get(device.image_id) : undefined
        return <Typography.Text title={image?.name}>{androidVersionLabel(image?.api_level ?? device.capabilities.apiLevel)}</Typography.Text>
      },
    },
    {
      title: '设备池', dataIndex: 'pool_name', width: 210, render: (value: string | undefined, device) => {
        const pool = device.pool_id ? poolByID.get(device.pool_id) : undefined
        const defaultImage = pool?.default_image_id ? imageByID.get(pool.default_image_id) : undefined
        return (
          <Space direction="vertical" size={0}>
            <Typography.Text>{value ?? pool?.name ?? '-'}</Typography.Text>
            {device.platform === 'android' && defaultImage && <Typography.Text type="secondary">默认 {androidVersionLabel(defaultImage.api_level)}</Typography.Text>}
          </Space>
        )
      },
    },
    { title: '扩容模板', dataIndex: 'is_pool_base', width: 100, render: (value: boolean | undefined) => value ? <Tag color="blue">扩容模板</Tag> : '-' },
    { title: '设备标识', dataIndex: 'serial', width: 170, ellipsis: true },
    { title: '设备类型', dataIndex: 'device_kind', width: 120, render: (value: string) => deviceKindLabel(value) },
    { title: '运行方式', dataIndex: 'provider_type', width: 130, render: (value: string) => providerTypeLabel(value) },
    { title: '设备状态', dataIndex: 'lifecycle_status', width: 110, render: (value: string) => <Tag color={lifecycleColor[value] ?? 'default'}>{lifecycleStatusLabel(value)}</Tag> },
    { title: '健康状态', dataIndex: 'health_status', width: 110, render: (value: string) => <Tag color={healthColor[value] ?? 'default'}>{healthStatusLabel(value)}</Tag> },
    { title: '配置 / 运行组件', dataIndex: 'reimage_status', width: 150, render: (value: string, device) => device.platform === 'ios' ? <Tag>CoreSimulator 已纳管</Tag> : value === 'pending'
      ? <Tag color="processing">正在换镜像</Tag>
      : value === 'failed' ? <Tag color="red" title={device.reimage_error}>上次重装失败</Tag> : <Tag>已生效</Tag> },
    { title: '设备维护方式', dataIndex: 'lifecycle_mode', width: 120, render: (value: string) => lifecycleModeLabel(value) },
    { title: '所属宿主机', dataIndex: 'host_id', width: 150, render: (value: string) => shortID(value) },
    { title: '自动化接入', dataIndex: 'adb_endpoint', width: 180, ellipsis: true, render: (value: string | undefined, device) => device.platform === 'ios' ? '按预约建立受控会话' : (value ?? '-') },
    { title: '状态说明', dataIndex: 'health_reason', width: 220, ellipsis: true, render: (value?: string) => healthReasonLabel(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    actionColumn,
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const deviceQueryOptions = { query: { refetchInterval: 5_000, refetchOnWindowFocus: true, refetchOnReconnect: true } }
  const platformFilter = platformView === 'all' ? {} : { platform: platformView }
  const availableCountQuery = useListDevices({ page: 1, page_size: 1, ...platformFilter, lifecycle_status: 'ready', health_status: 'healthy' }, deviceQueryOptions)
  const busyCountQuery = useListDevices({ page: 1, page_size: 1, ...platformFilter, lifecycle_status: 'busy' }, deviceQueryOptions)
  const quarantinedCountQuery = useListDevices({ page: 1, page_size: 1, ...platformFilter, lifecycle_status: 'quarantined' }, deviceQueryOptions)
  const deletedCountQuery = useListDevices({ page: 1, page_size: 1, ...platformFilter, lifecycle_status: 'deleted' }, deviceQueryOptions)
  const allCountQuery = useListDevices({ page: 1, page_size: 1, ...platformFilter }, deviceQueryOptions)
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
  const { data, isLoading } = useListDevices({ page, page_size: pageSize, ...platformFilter, ...viewFilter }, deviceQueryOptions)
  const result = unwrapPage<Device>(data)

  return (
    <>
      <Space direction="vertical" size={14} style={{ display: 'flex' }}>
        <Card size="small" variant="borderless" styles={{ body: { padding: 0 } }} extra={role === 'admin' ? <Space>
          <Button onClick={openCreateDevice}>新增 Android 模拟器</Button>
          <Button type="primary" onClick={openCreateIOS}>新增 iOS 模拟器</Button>
        </Space> : undefined} title="Android 与 iOS 设备">
          <Typography.Text type="secondary">Android 模拟器与 iOS 模拟器都由各自宿主机按需创建；重建或删除会清空对应虚拟设备数据，普通预约释放不会自动清空。</Typography.Text>
        </Card>
        {provisioningState && <Alert
          type={provisioningState.status === 'failed' ? 'error' : provisioningState.status === 'ready' ? 'success' : 'info'}
          showIcon
          message={`设备创建进度：${({ preparing_image: '准备系统镜像', waiting_capacity: '等待宿主机容量', creating_emulator: '创建模拟器', adb_check: 'ADB 检查', stf_registration: 'STF 注册', appium_check: 'Appium 检查', ready: '可用', failed: '失败' } as Record<string, string>)[provisioningState.status] ?? '未知状态'}`}
          description={provisioningState.status === 'waiting_capacity' ? `${capacityMessage}（请求编号：${provisioningRequestID}）` : provisioningState.status === 'failed' ? `设备创建没有完成（错误代码：${provisioningState.error_code ?? '未知'}；请求编号：${provisioningRequestID}）。` : `可关闭页面；创建流程由服务端持续执行。（请求编号：${provisioningRequestID}）`}
        />}
        <Alert
          type="info"
          showIcon
          message="这里先显示可用设备"
          description="使用中、隔离和已删除设备可通过下方分类查看。"
        />
        <Segmented<PlatformView>
          value={platformView}
          options={[
            { label: '全部平台', value: 'all' },
            { label: 'Android', value: 'android' },
            { label: 'iOS', value: 'ios' },
          ]}
          onChange={(nextPlatform) => {
            const nextSearchParams = new URLSearchParams(searchParams)
            if (nextPlatform === 'all') nextSearchParams.delete('platform')
            else nextSearchParams.set('platform', nextPlatform)
            setSearchParams(nextSearchParams, { replace: true })
            onPageChange(1, pageSize)
          }}
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
        open={createDevice}
        title="新增 Android 模拟器"
        footer={[
          <Button key="cancel" onClick={() => setCreateDevice(false)}>取消</Button>,
          createStep > 0 && <Button key="previous" onClick={() => setCreateStep((current) => current - 1)}>上一步</Button>,
          createStep < 3
            ? <Button key="next" type="primary" onClick={() => {
              const field = createStep === 0 ? 'hardware_profile_id' : createStep === 1 ? 'catalog_id' : 'pool_id'
              void createForm.validateFields([field]).then(() => setCreateStep((current) => current + 1))
            }}>下一步</Button>
            : <Button key="create" type="primary" loading={provision.isPending} onClick={() => createForm.submit()}>创建设备</Button>,
        ]}
        cancelText="取消"
        onCancel={() => setCreateDevice(false)}
        width={900}
        destroyOnHidden
      >
        <Steps current={createStep} size="small" style={{ marginBottom: 20 }} items={[{ title: '选择 Phone' }, { title: '选择 Android' }, { title: '选择设备池' }, { title: '高级配置' }]} />
        <Form<CreateDeviceValues> form={createForm} layout="vertical" onFinish={submitCreateDevice}>
          <Form.Item name="hardware_profile_id" hidden rules={[{ required: true, message: '请选择 Phone 模板' }]}><Input /></Form.Item>
          <Form.Item name="catalog_id" hidden rules={[{ required: true, message: '请选择 Android 版本' }]}><Input /></Form.Item>
          {createStep === 0 && <>
            <Input.Search placeholder="搜索 Pixel 或 Phone 型号" value={profileSearch} onChange={(event) => setProfileSearch(event.target.value)} style={{ marginBottom: 12 }} />
            <Table<AndroidHardwareProfile> size="small" loading={hardwareQuery.isFetching} rowKey="id" pagination={{ pageSize: 8 }} dataSource={filteredHardwareProfiles} rowSelection={{ type: 'radio', selectedRowKeys: [createForm.getFieldValue('hardware_profile_id')].filter(Boolean), onChange: (keys) => {
              const id = String(keys[0] ?? '')
              const profile = hardwareProfiles.find((item) => item.id === id)
              createForm.setFieldsValue({ hardware_profile_id: id, width: profile?.width, height: profile?.height, density_dpi: profile?.density_dpi })
            } }} columns={[{ title: 'Phone 名称', dataIndex: 'name' }, { title: '宽', dataIndex: 'width', width: 90 }, { title: '高', dataIndex: 'height', width: 90 }, { title: 'DPI', dataIndex: 'density_dpi', width: 90 }, { title: '最低 API', width: 100, render: () => '26+' }]} />
          </>}
          {createStep === 1 && <>
            <Typography.Paragraph type="secondary">Android 系统列表已经收进新增设备流程。未缓存版本也可选择，服务端会继续完成“准备系统镜像 → 创建模拟器 → ADB → STF → Appium”，无需保持此页面开启。</Typography.Paragraph>
            <Form.Item name="catalog_id" label="Android 系统版本" rules={[{ required: true, message: '请选择 Android 系统版本' }]}>
              <Select
                showSearch
                optionFilterProp="label"
                loading={catalogQuery.isFetching}
                placeholder={catalog.length > 0 ? '选择 Android 系统版本' : '当前没有可选的 Android 系统版本'}
                options={catalog.map((image) => ({ value: image.id, label: androidCatalogOptionLabel(image) }))}
              />
            </Form.Item>
          </>}
          {createStep === 2 && <Form.Item name="pool_id" label="Android 设备池" rules={[{ required: true, message: '请选择活动的 Android 设备池' }]}>
            <Select loading={poolsQuery.isFetching} placeholder={androidPools.length > 0 ? '选择 Android 设备池' : '当前没有活动的 Android 设备池'} options={androidPools.map((pool) => ({ value: pool.id, label: `${pool.name} · 目标 ${pool.total_target} · ${pool.base_device_id ? '已设置扩容模板' : '待设置扩容模板'}` }))} />
          </Form.Item>}
          {createStep === 3 && <Collapse defaultActiveKey={['runtime']} items={[{ key: 'runtime', label: '高级选项（CPU、内存、磁盘、分辨率和 GPU）', children: <Space wrap align="start">
            <Form.Item name="container_cpu_cores" label="容器 CPU（核）" rules={[{ required: true }]}><InputNumber min={1} max={64} /></Form.Item>
            <Form.Item name="container_memory_mb" label="容器内存（MiB）" rules={[{ required: true }]}><InputNumber min={2048} max={262144} step={512} /></Form.Item>
            <Form.Item name="guest_cpu_cores" label="Android CPU（核）" rules={[{ required: true }]}><InputNumber min={1} max={32} /></Form.Item>
            <Form.Item name="guest_memory_mb" label="Android 内存（MiB）" rules={[{ required: true }]}><InputNumber min={1536} step={512} /></Form.Item>
            <Form.Item name="data_disk_mb" label="设备数据盘（MiB）" rules={[{ required: true }]}><InputNumber min={2048} step={1024} /></Form.Item>
            <Form.Item name="width" label="分辨率宽" rules={[{ required: true }]}><InputNumber min={320} /></Form.Item>
            <Form.Item name="height" label="分辨率高" rules={[{ required: true }]}><InputNumber min={480} /></Form.Item>
            <Form.Item name="density_dpi" label="DPI" rules={[{ required: true }]}><InputNumber min={120} max={960} /></Form.Item>
            <Form.Item name="vm_heap_mb" label="VM Heap（MiB）" rules={[{ required: true }]}><InputNumber min={128} /></Form.Item>
            <Form.Item name="graphics" label="图形模式" rules={[{ required: true }]}><Select style={{ width: 130 }} options={[{ value: 'auto', label: '自动' }, { value: 'host', label: '宿主机 GPU' }, { value: 'software', label: '软件渲染' }]} /></Form.Item>
          </Space> }]} />}
        </Form>
      </Modal>
      <Modal
        open={createIOS}
        title="新增 iOS 模拟器"
        width={760}
        destroyOnHidden
        onCancel={() => setCreateIOS(false)}
        afterOpenChange={initializeIOSCreateForm}
        footer={[
          <Button key="cancel-ios" onClick={() => setCreateIOS(false)}>取消</Button>,
          iosCreateStep > 0 && <Button key="previous-ios" onClick={() => setIOSCreateStep((step) => step - 1)}>上一步</Button>,
          iosCreateStep < 2
            ? <Button key="next-ios" type="primary" onClick={() => {
              const fields: (keyof CreateIOSSimulatorValues)[] = iosCreateStep === 0 ? ['host_id'] : ['runtime_id', 'device_type_id']
              void iosForm.validateFields(fields).then(() => setIOSCreateStep((step) => step + 1)).catch(() => undefined)
            }}>下一步</Button>
            : <Button key="create-ios" type="primary" loading={createIOSSimulator.isPending} onClick={() => iosForm.submit()}>创建模拟器</Button>,
        ]}
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="iOS 使用 Mac 宿主机内的 Xcode CoreSimulator"
          description="系统会在后台创建、启动并登记虚拟 iPhone；不安装第二层 macOS 虚拟机，也不向浏览器暴露 Appium、WDA 或会话授权。"
        />
        <Steps current={iosCreateStep} size="small" style={{ marginBottom: 20 }} items={[{ title: '选择 Mac' }, { title: '选择系统与机型' }, { title: '设备池与审计' }]} />
        <Form<CreateIOSSimulatorValues> form={iosForm} layout="vertical" onFinish={() => submitCreateIOS(iosForm.getFieldsValue(true) as CreateIOSSimulatorValues)}>
          {iosCreateStep === 0 && <Form.Item name="host_id" label="可用 Mac 宿主机" rules={[{ required: true, message: '请选择在线且未排空的 Mac 宿主机' }]}>
            <Select
              loading={hostsQuery.isFetching}
              placeholder={iosHosts.length > 0 ? '选择 Mac 宿主机' : '当前没有可用于创建的 Mac 宿主机'}
              options={iosHosts.map((host) => ({ value: host.id, label: `${host.name} · ${host.host_arch} · ${shortID(host.id)}` }))}
              onChange={() => iosForm.setFieldsValue({ runtime_id: undefined, device_type_id: undefined })}
            />
          </Form.Item>}
          {iosCreateStep === 1 && <>
            {iosCatalogQuery.isError && <Alert type="error" showIcon message="无法读取这台 Mac 的 iOS 目录，请检查宿主机在线状态后重试" style={{ marginBottom: 12 }} />}
            <Form.Item name="runtime_id" label="iOS 运行时" rules={[{ required: true, message: '请选择 iOS 运行时' }]}>
              <Select loading={iosCatalogQuery.isFetching} placeholder="选择宿主机已安装的 iOS 运行时" options={(iosCatalog?.runtimes ?? []).map((runtime, index) => ({ value: runtime.id, label: `${runtime.name} · ${runtime.version}${index === 0 ? '（默认）' : ''}` }))} onChange={() => iosForm.setFieldValue('device_type_id', undefined)} />
            </Form.Item>
            <Form.Item name="device_type_id" label="iPhone 机型" rules={[{ required: true, message: '请选择 iPhone 机型' }]}>
              <Select loading={iosCatalogQuery.isFetching} disabled={!selectedIOSRuntimeID} showSearch optionFilterProp="label" placeholder={selectedIOSRuntimeID ? '选择与当前 iOS 运行时兼容的 iPhone 机型' : '请先选择 iOS 运行时'} options={compatibleIOSDeviceTypes.map((deviceType, index) => ({ value: deviceType.id, label: `${deviceType.name}${index === 0 ? '（默认模板）' : ''}` }))} />
            </Form.Item>
          </>}
          {iosCreateStep === 2 && <>
            <Form.Item name="pool_id" label="iOS 设备池" rules={[{ required: true, message: '请选择活动的 iOS 设备池' }]}>
              <Select placeholder={iosPools.length > 0 ? '选择 iOS 设备池' : '当前没有活动的 iOS 设备池'} options={iosPools.map((pool) => ({ value: pool.id, label: `${pool.name} · 当前目标 ${pool.total_target}` }))} />
            </Form.Item>
            <Form.Item name="display_name" label="显示名称（可选）">
              <Input maxLength={128} placeholder="例如：iOS 26 回归机" />
            </Form.Item>
            <Form.Item name="reason" label="创建原因（必填，将写入审计）" rules={[{ required: true, whitespace: true, message: '请填写创建原因' }, { min: 3, message: '创建原因至少填写 3 个字' }]}>
              <Input.TextArea rows={3} maxLength={200} placeholder="例如：新增 iOS 26 自动化验证设备" />
            </Form.Item>
          </>}
        </Form>
      </Modal>
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
          {actionState?.action === 'start' && '启动会在 Mac 宿主机中拉起这台 CoreSimulator 虚拟 iPhone。'}
          {actionState?.action === 'stop' && '停止只关闭空闲的 CoreSimulator，不删除设备和数据。'}
          {actionState?.action === 'rebuild' && (actionState.device.platform === 'ios' ? '重建会关闭、擦除并重新启动 CoreSimulator，UDID 保持不变但设备数据全部清空。' : '重建会销毁并重新拉起设备运行实例，属于危险操作。')}
          {actionState?.action === 'restart' && '重启会中断当前设备上的会话。'}
          {actionState?.action === 'delete' && (actionState.device.platform === 'ios' ? '删除只允许没有活动预约或会话的受管 Simulator；成功后 CoreSimulator UDID 将消失并保留审计记录。' : '删除允许空闲、隔离或已停止且没有活动预约的设备。成功后会清理运行资源并转入已删除历史，同时把设备池目标数量减少一台，不会自动补建。')}
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
