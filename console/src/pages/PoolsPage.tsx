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
  useListDeviceImages,
  useListDevicePools,
  useListDevices,
  useSelectDevicePoolBaseDevice,
  useUpdateDevicePool,
} from '../api/generated/device-farm'
import type { ConsoleRole, DevicePool, Device, DeviceImage } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { androidVersionLabel, formatTime, shortID } from '../api/format'
import { lifecycleStatusLabel, poolStatusLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'

interface PoolFormValues {
  name: string
  default_lease_seconds: number
  max_lease_seconds: number
  device_count: number
  reason: string
}

function errorText(error: unknown): string {
  const err = error as { code?: string; requestId?: string; message?: string }
  return `${err.code ?? '未知错误'}（请求编号：${err.requestId ?? '-'}）：${err.message ?? '请稍后重试'}`
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
  const devicesQuery = useListDevices({ page: 1, page_size: 200, pool_id: configPool?.id }, { query: { enabled: Boolean(configPool) } })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []

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
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          const updated = (data as unknown as { data?: DevicePool } | undefined)?.data
          if (updated) {
            setConfigPool(updated)
            poolForm.setFieldsValue({ device_count: updated.total_target, reason: '' })
          }
          message.success(`池配置已更新（request_id: ${requestID}）`)
          invalidatePools()
          invalidateDevices()
        },
        onError: (error) => message.error(`更新失败：${errorText(error)}`),
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
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`设备已加入池（request_id: ${requestID}）`)
          setAddDeviceOpen(false)
          invalidatePools()
        },
        onError: (error) => message.error(`加入失败：${errorText(error)}`),
      },
    )
  }

  const setBaseDevice = (deviceID: string) => {
    if (!configPool) return
    selectBaseDevice.mutate({ id: configPool.id, data: { device_id: deviceID, reason: '选择后续扩容的基础设备' } }, {
      onSuccess: (data) => {
        const updated = (data as unknown as { data?: DevicePool }).data
        if (updated) setConfigPool(updated)
        message.success('基础设备已更新；后续扩容将使用它的镜像和资源配置')
        invalidatePools()
      },
      onError: (error) => message.error(`设置基础设备失败：${errorText(error)}`),
    })
  }

  const columns: TableColumnsType<DevicePool> = [
    { title: '设备池编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: '平台', dataIndex: 'platform', width: 90, render: (value: string) => <Tag color={value === 'ios' ? 'blue' : 'green'}>{value === 'ios' ? 'iOS' : '安卓'}</Tag> },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{poolStatusLabel(value)}</Tag> },
    { title: '默认租期（秒）', dataIndex: 'default_lease_seconds', width: 130 },
    { title: '最长租期（秒）', dataIndex: 'max_lease_seconds', width: 130 },
    { title: '设备数量', dataIndex: 'total_target', width: 100 },
    {
      title: '默认系统', dataIndex: 'default_image_id', width: 180,
      render: (value: string | undefined, pool) => pool.platform === 'ios' ? '创建时选择 Runtime' : value && imageByID.get(value) ? androidVersionLabel(imageByID.get(value)?.api_level) : '-',
    },
    { title: '基础设备', dataIndex: 'base_device_id', width: 150, render: (value?: string) => value ? shortID(value) : <Tag>未选择</Tag> },
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
          <Form.Item name="name" label="设备池名称" extra={configPool?.platform === 'ios' ? '名称只是管理标识；iOS Runtime 和机型以每台 Simulator 的创建记录为准。' : '名称只是管理标识，不代表当前 Android 版本；系统版本以“默认系统”和设备列表为准。'} rules={[{ required: true, message: '请输入池名称' }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Space size={16} wrap>
            <Form.Item name="default_lease_seconds" label="默认租期（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400} />
            </Form.Item>
            <Form.Item name="max_lease_seconds" label="最长租期（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400 * 7} />
            </Form.Item>
            <Form.Item name="device_count" label="设备数量" extra={configPool?.platform === 'ios' ? 'iOS 设备通过设备页按需创建或删除。' : '调大自动扩容，调小自动缩容。'} rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} disabled={configPool?.platform === 'ios'} />
            </Form.Item>
          </Space>
          <Typography.Paragraph type="secondary">
            {configPool?.platform === 'ios'
              ? 'iOS Pool 只管理预约和并发；虚拟 iPhone 在设备页选择 Mac、Runtime 和机型后按需创建，删除时目标数量自动同步。'
              : '设备数量决定这个池保留多少台可用设备。扩容会沿用基础设备的系统版本和硬件规格创建全新设备；缩容只处理空闲设备，不会中断正在运行的任务。'}
          </Typography.Paragraph>
          {configPool && (
            <Alert
              showIcon
              style={{ marginBottom: 16 }}
              type={currentPoolDevices === (desiredDeviceCount ?? configPool.total_target) ? 'success' : 'info'}
              message={`当前 ${currentPoolDevices} 台，目标 ${(desiredDeviceCount ?? configPool.total_target)} 台`}
              description={currentPoolDevices < (desiredDeviceCount ?? configPool.total_target)
                ? configPool.base_device_id
                  ? `保存后将自动创建 ${(desiredDeviceCount ?? configPool.total_target) - currentPoolDevices} 台；容量不足时页面会显示具体原因。`
                  : '扩容前请先在下方选择一台健康的基础设备。'
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

        {configPool?.platform === 'android' && <>
        <Typography.Title level={5} style={{ marginTop: 24 }}>设备</Typography.Title>
        <Typography.Paragraph type="secondary">基础设备决定后续扩容的配置，不会共享或复制这台设备内的数据。</Typography.Paragraph>
        <Select
          style={{ width: '100%', marginBottom: 12 }}
          value={configPool?.base_device_id}
          placeholder="选择基础设备"
          loading={devicesQuery.isFetching || selectBaseDevice.isPending}
          onChange={setBaseDevice}
          options={devices.filter((device) => device.lifecycle_status === 'ready' && device.health_status === 'healthy' && device.device_kind === 'emulator' && device.provider_type === 'docker_emulator').map((device) => ({
            value: device.id, label: `${shortID(device.id)} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`,
          }))}
        />
        <Button size="small" onClick={() => setAddDeviceOpen(true)}>加入设备</Button>
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
        destroyOnHidden
      >
        <DeviceSelect devices={devices} loading={devicesQuery.isFetching} onChange={(id) => setSelectedDevice(id)} />
      </Modal>
    </>
  )
}

function DeviceSelect({ devices, loading, onChange }: { devices: Device[]; loading: boolean; onChange: (id: string) => void }) {
  return (
    <Select
      showSearch
      style={{ width: '100%' }}
      placeholder="选择设备（设备 ID · 序列号）"
      optionFilterProp="label"
      loading={loading}
      onChange={onChange}
      options={devices.map((device) => ({
        value: device.id,
        label: `${shortID(device.id)} · ${device.serial} · ${lifecycleStatusLabel(device.lifecycle_status)}`,
      }))}
    />
  )
}
