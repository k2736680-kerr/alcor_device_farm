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
import { useState } from 'react'
import {
  getListDevicePoolsQueryKey,
  getListDevicesQueryKey,
  useAddDeviceToPool,
  useListDevicePools,
  useListDevices,
  useSelectDevicePoolBaseDevice,
  useUpdateDevicePool,
} from '../api/generated/device-farm'
import type { DevicePool, Device } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { lifecycleStatusLabel, poolStatusLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'

interface PoolFormValues {
  name: string
  default_lease_seconds: number
  max_lease_seconds: number
  max_concurrency: number
  total_target: number
  min_ready: number
  reason: string
}

function errorText(error: unknown): string {
  const err = error as { code?: string; requestId?: string; message?: string }
  return `${err.code ?? '未知错误'}（请求编号：${err.requestId ?? '-'}）：${err.message ?? '请稍后重试'}`
}

export function PoolsPage() {
  const { message, modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [configPool, setConfigPool] = useState<DevicePool | null>(null)
  const [addDeviceOpen, setAddDeviceOpen] = useState(false)
  const [selectedDevice, setSelectedDevice] = useState<string | null>(null)
  const [poolForm] = Form.useForm<PoolFormValues>()

  const updatePool = useUpdateDevicePool()
  const addDevice = useAddDeviceToPool()
  const selectBaseDevice = useSelectDevicePoolBaseDevice()

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
      { id: configPool.id, data: { ...values, platform: configPool.platform } },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          const updated = (data as unknown as { data?: DevicePool } | undefined)?.data
          if (updated) {
            setConfigPool(updated)
            poolForm.setFieldValue('reason', '')
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
    if (values.max_concurrency > values.total_target) {
      poolForm.setFields([{ name: 'max_concurrency', errors: ['最大并发不能超过总目标数量'] }])
      return
    }
    if (values.min_ready > values.total_target) {
      poolForm.setFields([{ name: 'min_ready', errors: ['最小预热不能超过总目标数量'] }])
      return
    }
    if (values.total_target < configPool.total_target) {
      if (values.reason.trim().length < 3) {
        poolForm.setFields([{ name: 'reason', errors: ['缩容时请填写至少 3 个字的调整原因'] }])
        return
      }
      modal.confirm({
        title: `确认把设备池总目标缩容到 ${values.total_target} 台？`,
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
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{poolStatusLabel(value)}</Tag> },
    { title: '默认租期（秒）', dataIndex: 'default_lease_seconds', width: 130 },
    { title: '最长租期（秒）', dataIndex: 'max_lease_seconds', width: 130 },
    { title: '总目标', dataIndex: 'total_target', width: 90 },
    { title: '最小预热', dataIndex: 'min_ready', width: 90 },
    { title: '最大并发', dataIndex: 'max_concurrency', width: 100 },
    { title: '基础设备', dataIndex: 'base_device_id', width: 150, render: (value?: string) => value ? shortID(value) : <Tag>未选择</Tag> },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      fixed: 'right',
      render: (_, pool) => (
        <Button
          size="small"
          onClick={() => {
            setConfigPool(pool)
            poolForm.setFieldsValue({
              name: pool.name,
              default_lease_seconds: pool.default_lease_seconds,
              max_lease_seconds: pool.max_lease_seconds,
              max_concurrency: pool.max_concurrency,
              total_target: pool.total_target,
              min_ready: pool.min_ready,
              reason: '',
            })
          }}
        >
          配置
        </Button>
      ),
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
        title={configPool ? `池配置 · ${configPool.name}` : ''}
        width={640}
        onClose={() => setConfigPool(null)}
      >
        <Typography.Title level={5}>基本信息</Typography.Title>
        <Form<PoolFormValues> form={poolForm} layout="vertical" onFinish={savePool}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入池名称' }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Space size={16} wrap>
            <Form.Item name="default_lease_seconds" label="默认租期（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400} />
            </Form.Item>
            <Form.Item name="max_lease_seconds" label="最长租期（秒）" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400 * 7} />
            </Form.Item>
            <Form.Item name="total_target" label="总目标数量" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} />
            </Form.Item>
            <Form.Item name="min_ready" label="最小预热数量" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} />
            </Form.Item>
            <Form.Item name="max_concurrency" label="最大并发" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} />
            </Form.Item>
          </Space>
          <Typography.Paragraph type="secondary">
            总目标是该池最多维持的设备总数。选定基础设备后，自动增加设备会复制它当前的镜像、Phone 模板和 CPU/内存等运行配置，但始终使用全新的空数据卷；不会复制 APK、帐号或缓存。基础设备修改配置后，下一次扩容自动生效。
          </Typography.Paragraph>
          {configPool && (
            <Alert
              showIcon
              style={{ marginBottom: 16 }}
              type={currentPoolDevices < configPool.min_ready ? 'warning' : 'info'}
              message={`当前 ${currentPoolDevices} 台 · 总目标 ${configPool.total_target} 台 · 最小预热 ${configPool.min_ready} 台`}
              description={currentPoolDevices < configPool.min_ready
                ? configPool.base_device_id
                  ? `尚缺 ${configPool.min_ready - currentPoolDevices} 台。系统会按基础设备当前配置自动补建；如果持续不变化，通常是宿主机实际 CPU、内存或 Docker 数据盘不足。`
                  : '该设备池尚未选择基础设备。历史默认镜像仅作为兼容兜底；请在下方选择一台健康 Phone 设备后再扩容。'
                : '当前设备数已达到最小预热要求；总目标仍是设备池允许维持的数量上限。'}
            />
          )}
          <Form.Item
            name="reason"
            label="调整原因（缩容时必填并写入审计）"
            dependencies={['total_target']}
            rules={[
              ({ getFieldValue }) => ({
                validator: (_, value?: string) => {
                  const target = Number(getFieldValue('total_target'))
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
          <Button type="primary" loading={updatePool.isPending} onClick={() => poolForm.submit()}>保存基本信息</Button>
        </Form>

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
