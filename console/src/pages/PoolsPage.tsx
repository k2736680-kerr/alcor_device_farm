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
import { androidVersionLabel, formatTime, shortID } from '../api/format'
import { lifecycleStatusLabel, poolStatusLabel } from '../api/labels'
import { apiErrorText, durationLabel, responseRequestID } from '../api/presentation'
import { PageTable } from '../components/PageTable'

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

function templateLabel(device: Device): string {
  const capabilities = device.capabilities as Record<string, unknown>
  if (device.platform === 'ios') {
    const model = String(capabilities.model ?? capabilities.deviceName ?? 'iPhone')
    const runtime = String(capabilities.runtimeId ?? '运行时未知')
    return `${shortID(device.id)} · ${model} · ${runtime}`
  }
  return `${shortID(device.id)} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`
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
    const poolRegistered = devices.filter((device) => device.host_id === host.id && !['deleted', 'quarantined'].includes(device.lifecycle_status)).length
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
  const devicesQuery = useListDevices({ page: 1, page_size: 200 }, { query: { enabled: Boolean(configPool) } })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []
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
          max_concurrency: values.device_count,
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
    { title: '设备池编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: '平台', dataIndex: 'platform', width: 90, render: (value: string) => <Tag color={value === 'ios' ? 'blue' : 'green'}>{value === 'ios' ? 'iOS' : 'Android'}</Tag> },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{poolStatusLabel(value)}</Tag> },
    { title: '默认租期', dataIndex: 'default_lease_seconds', width: 130, render: (value: number) => durationLabel(value) },
    { title: '最长租期窗口', dataIndex: 'max_lease_seconds', width: 140, render: (value: number) => durationLabel(value) },
    { title: '目标设备数', dataIndex: 'total_target', width: 110 },
    {
      title: '默认系统', dataIndex: 'default_image_id', width: 180,
      render: (value: string | undefined, pool) => pool.platform === 'ios' ? '由扩容模板决定' : value && imageByID.get(value) ? androidVersionLabel(imageByID.get(value)?.api_level) : '-',
    },
    { title: '扩容模板', dataIndex: 'base_device_id', width: 150, render: (value?: string) => value ? shortID(value) : <Tag>未选择</Tag> },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
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
  const { data, isLoading } = useListDevicePools(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DevicePool>(data)

  return (
    <>
      <PageTable<DevicePool>
        columns={columns}
        dataSource={result?.items}
        loading={isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
      />

      <Drawer
        open={configPool !== null}
        title={configPool ? `设备池设置 · ${configPool.name}` : ''}
        width={640}
        onClose={() => setConfigPool(null)}
      >
        <Typography.Title level={5}>基本信息</Typography.Title>
        <Form<PoolFormValues> form={poolForm} layout="vertical" onFinish={savePool}>
          <Form.Item name="name" label="设备池名称" extra={configPool?.platform === 'ios' ? '名称只是管理标识；iOS 运行时和机型由扩容模板决定。' : '名称只是管理标识，不代表当前 Android 版本；系统版本以“默认系统”和设备列表为准。'} rules={[{ required: true, message: '请输入池名称' }]}>
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
              type={currentPoolDevices === (desiredDeviceCount ?? configPool.total_target) ? 'success' : 'info'}
              message={`当前 ${currentPoolDevices} 台，目标 ${(desiredDeviceCount ?? configPool.total_target)} 台`}
              description={currentPoolDevices < (desiredDeviceCount ?? configPool.total_target)
                ? scaleUpDescription(configPool, currentPoolDevices, desiredDeviceCount ?? configPool.total_target, poolDevices, baseHost)
                : currentPoolDevices > (desiredDeviceCount ?? configPool.total_target)
                  ? `保存后将安全移除 ${currentPoolDevices - (desiredDeviceCount ?? configPool.total_target)} 台空闲设备；使用中的设备会在任务结束后处理。`
                  : '当前数量与目标一致，无需扩容或缩容。'}
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
        <Select
          style={{ width: '100%', marginBottom: 12 }}
          value={configPool?.base_device_id}
          placeholder="选择健康的扩容模板"
          loading={devicesQuery.isFetching || selectBaseDevice.isPending}
          onChange={setBaseDevice}
          options={poolDevices.filter((device) => device.lifecycle_status === 'ready' && device.health_status === 'healthy' && (
            configPool.platform === 'ios'
              ? device.platform === 'ios' && device.device_kind === 'simulator' && device.provider_type === 'appium_device_farm_ios'
              : device.platform === 'android' && device.device_kind === 'emulator' && device.provider_type === 'docker_emulator'
          )).map((device) => ({
            value: device.id, label: templateLabel(device),
          }))}
        />
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
        <DeviceSelect devices={addableDevices} loading={devicesQuery.isFetching} onChange={(id) => setSelectedDevice(id)} />
      </Modal>
    </>
  )
}

function DeviceSelect({ devices, loading, onChange }: { devices: Device[]; loading: boolean; onChange: (id: string) => void }) {
  return (
    <Select
      showSearch
      style={{ width: '100%' }}
      placeholder="选择设备（设备编号 · 设备标识）"
      optionFilterProp="label"
      loading={loading}
      notFoundContent={loading ? '正在加载设备…' : '没有可加入的设备'}
      onChange={onChange}
      options={devices.map((device) => ({
        value: device.id,
        label: `${shortID(device.id)} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`,
      }))}
    />
  )
}
