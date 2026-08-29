import type { ReactNode } from 'react'
import {
  App as AntApp,
  Alert,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  getListDevicePoolsQueryKey,
  getListDevicesQueryKey,
  useAddDeviceToPool,
  useListDeviceHosts,
  useListDeviceImages,
  useListDevicePools,
  useListDevices,
  useSelectDevicePoolBaseDevice,
  useUpdateDevicePool,
} from '../api/generated/device-farm'
import type { ConsoleRole, DevicePool, Device, DeviceHost, DeviceImage } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { androidVersionLabel, shortID } from '../api/format'
import { deviceHeadline, deviceModelLabel, deviceSystemLabel, hostLabel } from '../api/describe'
import { lifecycleStatusLabel, poolStatusLabel } from '../api/labels'
import { apiErrorText, durationLabel, responseRequestID } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { PageQueryError, ResourcePageHeader } from '../components/ResourcePage'

interface PoolFormValues {
  name: string
  default_lease_seconds: number
  max_lease_seconds: number
  device_count: number
  reason: string
}

function numeric(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

interface TemplateOption {
  value: string
  label: ReactNode
}

/**
 * Two-line scale-out template option.
 * The first line answers "which OS and which model", the second "where it runs".
 * Never lead with a bare identifier — operators cannot map an id to a system.
 */
function templateOption(device: Device, imageByID: Map<string, DeviceImage>, hostByID: Map<string, DeviceHost>): TemplateOption {
  return {
    value: device.id,
    label: (
      <span className="resource-option">
        <span className="resource-option-title">{deviceHeadline(device, { imageByID })}</span>
        <span className="resource-option-meta">
          {device.serial} · 宿主机 {hostLabel(device.host_id, hostByID)} · {shortID(device.id)}
        </span>
      </span>
    ),
  }
}

function templateHostDescription(host?: DeviceHost): string {
  if (!host) return '尚未读取到扩容模板所在宿主机。'
  if (host.status !== 'online') return `模板宿主机“${host.name}”当前离线，恢复在线后才会继续扩容。`
  if (host.draining) return `模板宿主机“${host.name}”正在排空，解除排空后才会继续扩容。`
  const capacity = host.capacity as Record<string, unknown>
  const memory = numeric(capacity.memory_available_mb)
  const disk = numeric(capacity.disk_available_mb)
  const slots = numeric(capacity.device_slots)
  if (memory === undefined && disk === undefined && slots === undefined) {
    return `模板宿主机“${host.name}”在线，正在等待资源心跳。`
  }
  return `模板宿主机“${host.name}”在线；可用内存 ${memory ?? '-'} MB，可用磁盘 ${disk ?? '-'} MB${slots ? `，设备安全上限 ${slots} 台` : ''}。`
}

function scaleUpDescription(pool: DevicePool, current: number, target: number, devices: Device[], host?: DeviceHost): string {
  const difference = target - current
  if (!pool.base_device_id) return '扩容前请先在下方选择一台健康的扩容模板。'
  if (!host) return `计划补齐 ${difference} 台设备，正在等待扩容模板所在宿主机的状态。`
  if (host.status !== 'online') return `计划补齐 ${difference} 台设备，但模板宿主机当前离线；恢复在线后自动继续。`
  if (host.draining) return `计划补齐 ${difference} 台设备，但模板宿主机正在排空；解除排空后自动继续。`
  if (pool.platform === 'ios') {
    const capacity = host.capacity as Record<string, unknown>
    const pending = devices.filter((device) => device.host_id === host.id && ['provisioning', 'booting'].includes(device.lifecycle_status)).length
    const poolRegistered = devices.filter((device) => device.host_id === host.id && device.lifecycle_status !== 'deleted').length
    const reportedUsed = numeric((host.used_capacity as Record<string, unknown>).device_slots) ?? 0
    const registered = Math.max(poolRegistered, reportedUsed)
    const memory = numeric(capacity.memory_available_mb)
    const disk = numeric(capacity.disk_available_mb)
    const slots = numeric(capacity.device_slots)
    const requiredMemory = 4096 * (pending + 1)
    const requiredDisk = 16384 * (pending + 1)
    const reasons: string[] = []
    if (memory !== undefined && memory < requiredMemory) reasons.push(`内存还缺 ${requiredMemory - memory} MB`)
    if (disk !== undefined && disk < requiredDisk) reasons.push(`磁盘还缺 ${requiredDisk - disk} MB`)
    if (slots !== undefined && registered >= slots) reasons.push('设备名额已达到安全上限')
    if (reasons.length > 0) return `还需补齐 ${difference} 台，但模板宿主机${reasons.join('、')}；释放资源后自动继续。`
  }
  return `正在按目标自动补齐 ${difference} 台设备；若宿主机资源暂时不足，目标值会保留并在资源恢复后继续。`
}

function capacityForPool(pool: DevicePool, devices: Device[]) {
  const members = devices.filter((device) => device.pool_id === pool.id && device.lifecycle_status !== 'deleted')
  const available = members.filter((device) => device.lifecycle_status === 'ready' && device.health_status === 'healthy').length
  const inUse = members.filter((device) => ['reserved', 'busy'].includes(device.lifecycle_status) && device.health_status === 'healthy').length
  const recovering = members.filter((device) => ['provisioning', 'booting'].includes(device.lifecycle_status)
    || (device.lifecycle_status === 'recycling' && device.health_status === 'healthy')).length
  const serviceable = available + inUse + recovering
  const faulted = Math.max(0, members.length - serviceable)
  return { registered: members.length, available, inUse, recovering, faulted, gap: Math.max(0, pool.total_target - serviceable) }
}

export function PoolsPage({ role = 'admin' }: { role?: ConsoleRole }) {
  const { message, modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [configPool, setConfigPool] = useState<DevicePool | null>(null)
  const [addDeviceOpen, setAddDeviceOpen] = useState(false)
  const [selectedDevice, setSelectedDevice] = useState<string | null>(null)
  const [poolForm] = Form.useForm<PoolFormValues>()
  const desiredDeviceCount = Form.useWatch('device_count', poolForm)

  const updatePool = useUpdateDevicePool()
  const addDevice = useAddDeviceToPool()
  const selectBaseDevice = useSelectDevicePoolBaseDevice()
  const imagesQuery = useListDeviceImages({ page: 1, page_size: 200, status: 'ready' })
  const images = unwrapPage<DeviceImage>(imagesQuery.data)?.items ?? []
  const imageByID = useMemo(() => new Map(images.map((image) => [image.id, image])), [images])
  const hostsQuery = useListDeviceHosts({ page: 1, page_size: 200 })
  const hosts = unwrapPage<DeviceHost>(hostsQuery.data)?.items ?? []
  const hostByID = useMemo(() => new Map(hosts.map((host) => [host.id, host])), [hosts])

  const invalidatePools = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicePoolsQueryKey() })
  }
  const invalidateDevices = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
  }

  const poolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id },
    {
      query: {
        enabled: Boolean(configPool),
        refetchInterval: 5_000,
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
      },
    },
  )
  const currentPoolDevices = unwrapPage<Device>(poolDevicesQuery.data)?.total ?? 0
  const poolStatusQueryOptions = {
    query: {
      enabled: Boolean(configPool), refetchInterval: 5_000,
      refetchOnWindowFocus: true, refetchOnReconnect: true,
    },
  }
  const availablePoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'ready', health_status: 'healthy' },
    poolStatusQueryOptions,
  )
  const reservedPoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'reserved', health_status: 'healthy' },
    poolStatusQueryOptions,
  )
  const busyPoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'busy', health_status: 'healthy' },
    poolStatusQueryOptions,
  )
  const provisioningPoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'provisioning' },
    poolStatusQueryOptions,
  )
  const bootingPoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'booting' },
    poolStatusQueryOptions,
  )
  const recyclingPoolDevicesQuery = useListDevices(
    { page: 1, page_size: 1, pool_id: configPool?.id, lifecycle_status: 'recycling', health_status: 'healthy' },
    poolStatusQueryOptions,
  )
  const availablePoolDevices = unwrapPage<Device>(availablePoolDevicesQuery.data)?.total ?? 0
  const inUsePoolDevices = (unwrapPage<Device>(reservedPoolDevicesQuery.data)?.total ?? 0)
    + (unwrapPage<Device>(busyPoolDevicesQuery.data)?.total ?? 0)
  const recoveringPoolDevices = (unwrapPage<Device>(provisioningPoolDevicesQuery.data)?.total ?? 0)
    + (unwrapPage<Device>(bootingPoolDevicesQuery.data)?.total ?? 0)
    + (unwrapPage<Device>(recyclingPoolDevicesQuery.data)?.total ?? 0)
  const serviceablePoolDevices = Math.min(currentPoolDevices, availablePoolDevices + inUsePoolDevices + recoveringPoolDevices)
  const faultedPoolDevices = Math.max(0, currentPoolDevices - serviceablePoolDevices)
  const devicesQuery = useListDevices({ page: 1, page_size: 200 })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []
  const deviceByID = useMemo(() => new Map(devices.map((device) => [device.id, device])), [devices])
  const poolDevices = devices.filter((device) => device.pool_id === configPool?.id)
  const addableDevices = devices.filter((device) => configPool && device.platform === configPool.platform && !device.pool_id && device.lifecycle_status !== 'deleted')
  const baseDevice = poolDevices.find((device) => device.id === configPool?.base_device_id)
  const baseHost = hostByID.get(baseDevice?.host_id ?? '')

  const updatePoolConfiguration = (values: PoolFormValues) => {
    if (!configPool) {
      return
    }
    updatePool.mutate(
      {
        id: configPool.id,
        data: {
          name: values.name,
          platform: configPool.platform,
          default_lease_seconds: values.default_lease_seconds,
          max_lease_seconds: values.max_lease_seconds,
          total_target: values.device_count,
          min_ready: values.device_count,
          max_concurrency: Math.max(1, values.device_count),
          reason: values.reason,
        },
      },
      {
        onSuccess: (data) => {
          const updated = (data as unknown as { data?: DevicePool } | undefined)?.data
          if (updated) {
            setConfigPool(updated)
            poolForm.setFieldsValue({ device_count: updated.total_target, reason: '' })
          }
          message.success(`设备池配置已更新（请求编号：${responseRequestID(data)}）`)
          invalidatePools()
          invalidateDevices()
        },
        onError: (error) => message.error(`更新失败：${apiErrorText(error)}`),
      },
    )
  }

  const savePool = (values: PoolFormValues) => {
    if (!configPool) {
      return
    }
    if (values.device_count < configPool.total_target) {
      if (values.reason.trim().length < 3) {
        poolForm.setFields([{ name: 'reason', errors: ['缩容时请填写至少 3 个字的调整原因'] }])
        return
      }
      modal.confirm({
        title: `确认把设备池缩容到 ${values.device_count} 台？`,
        content: '系统会删除最旧的空闲模拟器并保留最新设备；正在占用的设备会等待释放，不会被强制中断。',
        okText: '确认缩容',
        okButtonProps: { danger: true },
        cancelText: '取消',
        onOk: () => updatePoolConfiguration(values),
      })
      return
    }
    updatePoolConfiguration(values)
  }

  const addToPool = (deviceID: string) => {
    if (!configPool) {
      return
    }
    addDevice.mutate(
      { id: configPool.id, data: { device_id: deviceID } },
      {
        onSuccess: (data) => {
          message.success(`设备已加入设备池（请求编号：${responseRequestID(data)}）`)
          setAddDeviceOpen(false)
          setSelectedDevice(null)
          invalidatePools()
          invalidateDevices()
        },
        onError: (error) => message.error(`加入失败：${apiErrorText(error)}`),
      },
    )
  }

  const setBaseDevice = (deviceID: string) => {
    if (!configPool) return
    selectBaseDevice.mutate({ id: configPool.id, data: { device_id: deviceID, reason: '选择设备池扩容模板' } }, {
      onSuccess: (data) => {
        const updated = (data as unknown as { data?: DevicePool }).data
        if (updated) setConfigPool(updated)
        message.success(`${configPool.platform === 'ios'
          ? '扩容模板已更新；后续扩容将沿用它的 Mac、iOS 运行时和 iPhone 机型'
          : '扩容模板已更新；后续扩容将沿用它的镜像和资源配置'}（请求编号：${responseRequestID(data)}）`)
        invalidatePools()
      },
      onError: (error) => message.error(`设置扩容模板失败：${apiErrorText(error)}`),
    })
  }

  const columns: TableColumnsType<DevicePool> = [
    {
      title: '设备池', dataIndex: 'name', width: 220, render: (value: string, pool) => (
        <div className="primary-resource">
          <Typography.Text strong>{value}</Typography.Text>
          <small>{pool.platform === 'ios' ? 'iOS' : 'Android'} · {shortID(pool.id)}</small>
        </div>
      ),
    },
    {
      title: '容量健康', key: 'capacity', width: 300, render: (_, pool) => {
        const capacity = capacityForPool(pool, devices)
        const healthy = capacity.gap === 0 && capacity.faulted === 0
        return (
          <Space direction="vertical" size={3}>
            <Space>
              <Tag color={healthy ? 'green' : 'red'}>{healthy ? '容量正常' : `缺口 ${capacity.gap} 台`}</Tag>
              {capacity.faulted > 0 && <Tag color="red">故障 {capacity.faulted}</Tag>}
            </Space>
            <Typography.Text className="table-secondary">
              目标 {pool.total_target} · 可用 {capacity.available} · 使用中 {capacity.inUse} · 恢复中 {capacity.recovering}
            </Typography.Text>
          </Space>
        )
      },
    },
    { title: '调度状态', dataIndex: 'status', width: 110, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{poolStatusLabel(value)}</Tag> },
    {
      title: '扩容模板 / 默认镜像', key: 'scaleout', width: 300,
      render: (_, pool) => {
        const template = pool.base_device_id ? deviceByID.get(pool.base_device_id) : undefined
        const defaultImage = pool.default_image_id ? imageByID.get(pool.default_image_id) : undefined
        return (
          <Space direction="vertical" size={2}>
            <span className="table-secondary">
              扩容模板：{template
                ? <Typography.Text>{deviceHeadline(template, { imageByID })}</Typography.Text>
                : <Typography.Text type="warning">未设置，自动扩容已暂停</Typography.Text>}
            </span>
            {pool.platform === 'android' && (
              <span className="table-secondary">
                默认镜像：{defaultImage
                  ? <Typography.Text>{androidVersionLabel(defaultImage.api_level)}</Typography.Text>
                  : <Typography.Text type="warning">未设置</Typography.Text>}
              </span>
            )}
            {template && (
              <span className="table-secondary">
                模板所在宿主机：{hostLabel(template.host_id, hostByID)}
              </span>
            )}
          </Space>
        )
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      fixed: 'right',
      render: (_, pool) => role === 'admin' ? (
        <Button
          size="small"
          onClick={() => {
            setConfigPool(pool)
            poolForm.setFieldsValue({
              name: pool.name,
              default_lease_seconds: pool.default_lease_seconds,
              max_lease_seconds: pool.max_lease_seconds,
              device_count: pool.total_target,
              reason: '',
            })
          }}
        >
          配置
        </Button>
      ) : <Typography.Text type="secondary">只读</Typography.Text>,
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const query = useListDevicePools(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DevicePool>(query.data)

  return (
    <Space direction="vertical" size={14} style={{ display: 'flex' }}>
      <ResourcePageHeader
        title="设备池"
        description="按池查看登记容量和实际可服务容量。故障设备仍保留在池中并占用登记名额，系统优先恢复原机，不会自动删除或补建替代设备。"
        dataUpdatedAt={Math.max(query.dataUpdatedAt, devicesQuery.dataUpdatedAt)}
        isFetching={query.isFetching || devicesQuery.isFetching}
        onRefresh={() => void Promise.all([query.refetch(), devicesQuery.refetch()])}
        autoRefreshText="每 10 秒自动更新"
      />
      {query.isError && <PageQueryError error={query.error} onRetry={() => void query.refetch()} />}
      {devicesQuery.isError && <PageQueryError error={devicesQuery.error} onRetry={() => void devicesQuery.refetch()} />}
      {(unwrapPage<Device>(devicesQuery.data)?.total ?? 0) > devices.length && (
        <Alert type="warning" showIcon message="设备数量超过当前页面统计范围" description="容量主表暂只统计前 200 台设备；打开单个设备池设置可读取服务端精确数量。" />
      )}
      <PageTable<DevicePool>
        columns={columns}
        dataSource={result?.items}
        loading={query.isLoading || devicesQuery.isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: '尚未创建设备池' }}
      />

      <Drawer
        open={configPool !== null}
        title={configPool ? `设备池设置 · ${configPool.name}` : ''}
        width="min(640px, 100vw)"
        onClose={() => setConfigPool(null)}
      >
        <Typography.Title level={5}>基本信息</Typography.Title>
        <Form<PoolFormValues> form={poolForm} layout="vertical" onFinish={savePool}>
          <Form.Item name="name" label="设备池名称" extra={configPool?.platform === 'ios' ? '名称只是管理标识；当前用于扩容的 iOS 版本和机型显示在列表“扩容配置”中。' : '名称只是管理标识；当前用于扩容的 Android 版本和机型显示在列表“扩容配置”中。'} rules={[{ required: true, message: '请输入池名称' }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Space size={16} wrap>
            <Form.Item name="default_lease_seconds" label="默认租期（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400} />
            </Form.Item>
            <Form.Item name="max_lease_seconds" label="最大租期窗口（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400 * 7} />
            </Form.Item>
            <Form.Item name="device_count" label="目标设备数" extra="调大后自动扩容，调小后安全缩容。" rules={[{ required: true, message: '请输入目标设备数' }]}>
              <InputNumber min={0} max={1000} />
            </Form.Item>
          </Space>
          <Typography.Paragraph type="secondary">
            {configPool?.platform === 'ios'
              ? '目标设备数决定这个池保留多少台 iOS 模拟器。扩容会沿用模板的 Mac、iOS 运行时和 iPhone 机型创建全新设备，但不会复制模板数据；缩容只处理空闲设备。'
              : '目标设备数决定这个池保留多少台 Android 设备。扩容会沿用模板的系统版本和硬件规格创建全新设备，但不会复制模板数据；缩容只处理空闲设备。'}
          </Typography.Paragraph>
          {configPool && (
            <Alert
              showIcon
              style={{ marginBottom: 16 }}
              type={serviceablePoolDevices === (desiredDeviceCount ?? configPool.total_target) && faultedPoolDevices === 0 ? 'success' : faultedPoolDevices > 0 ? 'warning' : 'info'}
              message={`目标 ${(desiredDeviceCount ?? configPool.total_target)} 台 · 可服务 ${serviceablePoolDevices} 台 · 可立即使用 ${availablePoolDevices} 台`}
              description={<Space direction="vertical" size={2}>
                <span>已登记 {currentPoolDevices} 台，恢复中 {recoveringPoolDevices} 台，故障 {faultedPoolDevices} 台，缺口 {Math.max(0, (desiredDeviceCount ?? configPool.total_target) - serviceablePoolDevices)} 台。</span>
                <span>{currentPoolDevices < (desiredDeviceCount ?? configPool.total_target)
                  ? scaleUpDescription(configPool, currentPoolDevices, desiredDeviceCount ?? configPool.total_target, poolDevices, baseHost)
                  : currentPoolDevices > (desiredDeviceCount ?? configPool.total_target)
                    ? `保存后将安全移除 ${currentPoolDevices - (desiredDeviceCount ?? configPool.total_target)} 台空闲设备；使用中的设备会在任务结束后处理。`
                    : faultedPoolDevices > 0
                      ? '故障设备仍保留在池中，系统会持续探测并优先重启原设备；不会自动删除、清空数据或创建替代设备。'
                      : recoveringPoolDevices > 0
                        ? '已登记设备正在恢复，完成后会重新进入可用列表。'
                        : '当前登记容量和可服务容量均达到目标。'}</span>
              </Space>}
            />
          )}
          <Form.Item
            name="reason"
            label="调整原因（缩容时必填并写入审计）"
            dependencies={['device_count']}
            rules={[
              ({ getFieldValue }) => ({
                validator: (_, value?: string) => {
                  const target = Number(getFieldValue('device_count'))
                  if (configPool && target < configPool.total_target && (value?.trim().length ?? 0) < 3) {
                    return Promise.reject(new Error('缩容时请填写至少 3 个字的调整原因'))
                  }
                  return Promise.resolve()
                },
              }),
            ]}
          >
            <Input.TextArea rows={2} maxLength={200} placeholder="扩容可不填；缩容例如：测试环境释放资源" />
          </Form.Item>
          <Button type="primary" loading={updatePool.isPending} onClick={() => poolForm.submit()}>保存设置</Button>
        </Form>

        {configPool && <>
        <Typography.Title level={5} style={{ marginTop: 24 }}>扩容模板</Typography.Title>
        <Typography.Paragraph type="secondary">
          {configPool.platform === 'ios'
            ? '模板决定后续扩容使用的 Mac、iOS 运行时和 iPhone 机型；每次都会创建全新的 CoreSimulator，不会复制模板数据。'
            : '模板决定后续扩容使用的 Android 镜像和硬件规格；每次都会创建全新设备，不会复制模板数据。'}
        </Typography.Paragraph>
        <div className="field-label">当前扩容模板</div>
        <Select
          style={{ width: '100%', marginBottom: 12 }}
          value={configPool?.base_device_id}
          placeholder="选择健康的扩容模板"
          loading={devicesQuery.isFetching || selectBaseDevice.isPending}
          onChange={setBaseDevice}
          popupMatchSelectWidth={false}
          options={poolDevices.filter((device) => device.lifecycle_status === 'ready' && device.health_status === 'healthy' && (
            configPool.platform === 'ios'
              ? device.platform === 'ios' && device.device_kind === 'simulator' && device.provider_type === 'appium_device_farm_ios'
              : device.platform === 'android' && device.device_kind === 'emulator' && device.provider_type === 'docker_emulator'
          )).map((device) => templateOption(device, imageByID, hostByID))}
        />
        {baseDevice && (
          <Alert
            showIcon
            type="success"
            style={{ marginBottom: 12 }}
            message={`扩容将沿用：${deviceModelLabel(baseDevice)} · ${deviceSystemLabel(baseDevice, imageByID)}`}
            description={`模板设备 ${baseDevice.serial} · 宿主机 ${hostLabel(baseDevice.host_id, hostByID)} · 设备编号 ${baseDevice.id}。只有健康设备才能作为模板；模板被删除或隔离后扩容会暂停，需要重新选择。`}
          />
        )}
        {!configPool.base_device_id && (
          <Alert
            showIcon
            type="warning"
            style={{ marginBottom: 12 }}
            message="尚未设置扩容模板"
            description="没有模板时系统不会自动补齐设备，目标设备数只会保留为待办。请在上方选择一台健康设备。"
          />
        )}
        {configPool.base_device_id && (
          <Alert
            showIcon
            type="info"
            message="扩容宿主机资源"
            description={templateHostDescription(baseHost)}
            style={{ marginBottom: 12 }}
          />
        )}
        {configPool.platform === 'android' && <Button size="small" onClick={() => setAddDeviceOpen(true)}>加入设备</Button>}
        </>}
      </Drawer>

      <Modal
        open={addDeviceOpen}
        title="加入设备到池"
        okText="加入"
        onCancel={() => setAddDeviceOpen(false)}
        onOk={() => {
          const id = selectedDevice
          if (id) {
            addToPool(id)
          }
        }}
        confirmLoading={addDevice.isPending}
        okButtonProps={{ disabled: selectedDevice === null }}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary">只列出尚未加入其他设备池、且与当前设备池平台一致的设备。</Typography.Paragraph>
        <DeviceSelect devices={addableDevices} imageByID={imageByID} loading={devicesQuery.isFetching} onChange={(id) => setSelectedDevice(id)} />
      </Modal>
    </Space>
  )
}

function DeviceSelect({ devices, imageByID, loading, onChange }: {
  devices: Device[]
  imageByID: ReadonlyMap<string, DeviceImage>
  loading: boolean
  onChange: (id: string) => void
}) {
  return (
    <Select
      showSearch
      style={{ width: '100%' }}
      placeholder="选择设备（机型 · 系统版本 · 设备标识）"
      optionFilterProp="label"
      loading={loading}
      notFoundContent={loading ? '正在加载设备…' : '没有可加入的设备'}
      onChange={onChange}
      options={devices.map((device) => ({
        value: device.id,
        label: `${deviceHeadline(device, { imageByID })} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`,
      }))}
    />
  )
}
