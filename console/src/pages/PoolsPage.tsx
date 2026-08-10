import {
  App as AntApp,
  Alert,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  getListDevicePoolImagesQueryKey,
  getListDevicePoolsQueryKey,
  getListDevicesQueryKey,
  useAddDeviceToPool,
  useDisableDevicePoolImageTarget,
  useListDeviceImages,
  useListDevicePoolImages,
  useListDevicePools,
  useListDevices,
  useUpdateDevicePool,
} from '../api/generated/device-farm'
import type { DevicePool, DevicePoolImage, Device, DeviceImage } from '../api/generated/models'
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
  default_image_id: string
  reason: string
}

function errorText(error: unknown): string {
  const err = error as { code?: string; requestId?: string; message?: string }
  return `${err.code ?? 'ERROR'}（request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`
}

export function PoolsPage() {
  const { message, modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [configPool, setConfigPool] = useState<DevicePool | null>(null)
  const [addDeviceOpen, setAddDeviceOpen] = useState(false)
  const [selectedDevice, setSelectedDevice] = useState<string | null>(null)
  const [poolForm] = Form.useForm<PoolFormValues>()

  const updatePool = useUpdateDevicePool()
  const disableTarget = useDisableDevicePoolImageTarget()
  const addDevice = useAddDeviceToPool()

  const invalidatePools = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicePoolsQueryKey() })
  }
  const invalidatePoolImages = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicePoolImagesQueryKey() })
  }
  const invalidateDevices = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicesQueryKey() })
  }

  const poolImagesQuery = useListDevicePoolImages(
    configPool?.id ?? '',
    { page: 1, page_size: 100 },
    { query: { enabled: Boolean(configPool) } },
  )
  const poolImages = unwrapPage<DevicePoolImage>(poolImagesQuery.data)?.items ?? []
  const imagesQuery = useListDeviceImages(
    { page: 1, page_size: 200 },
    { query: { enabled: Boolean(configPool) } },
  )
  const images = unwrapPage<DeviceImage>(imagesQuery.data)?.items ?? []
  const imageByID = new Map(images.map((image) => [image.id, image]))
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
  const devicesQuery = useListDevices({ page: 1, page_size: 200 }, { query: { enabled: addDeviceOpen } })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []

  const updatePoolConfiguration = (values: PoolFormValues) => {
    if (!configPool) {
      return
    }
    updatePool.mutate(
      { id: configPool.id, data: values },
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

  const disable = (target: DevicePoolImage) => {
    if (!configPool) {
      return
    }
    disableTarget.mutate(
      { id: configPool.id, imageId: target.image_id },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`镜像目标已停用（request_id: ${requestID}）`)
          invalidatePoolImages()
          invalidateDevices()
        },
        onError: (error) => message.error(`停用失败：${errorText(error)}`),
      },
    )
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

  const targetColumns: TableColumnsType<DevicePoolImage> = [
    { title: '镜像', dataIndex: 'image_id', width: 230, render: (value: string) => imageByID.get(value)?.name ?? shortID(value) },
    { title: 'Android', dataIndex: 'image_id', width: 90, render: (value: string) => imageByID.get(value) ? `API ${imageByID.get(value)?.api_level}` : '-' },
    { title: '默认', dataIndex: 'image_id', width: 70, render: (value: string) => (value === configPool?.default_image_id ? <Tag color="blue">默认</Tag> : <Tag>可选</Tag>) },
    { title: '启用', dataIndex: 'enabled', width: 70, render: (value: boolean) => (value ? <Tag color="green">是</Tag> : <Tag>否</Tag>) },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, target) => (
        <Space size={4}>
          {target.enabled && target.image_id !== configPool?.default_image_id && (
            <Popconfirm title="停用该镜像目标？" okText="停用" onConfirm={() => disable(target)}>
              <Button size="small" danger>停用</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  const columns: TableColumnsType<DevicePool> = [
    { title: '设备池编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{poolStatusLabel(value)}</Tag> },
    { title: '默认租期（秒）', dataIndex: 'default_lease_seconds', width: 130 },
    { title: '最长租期（秒）', dataIndex: 'max_lease_seconds', width: 130 },
    { title: '总目标', dataIndex: 'total_target', width: 90 },
    { title: '最小预热', dataIndex: 'min_ready', width: 90 },
    { title: '最大并发', dataIndex: 'max_concurrency', width: 100 },
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
              default_image_id: pool.default_image_id ?? '',
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
              <InputNumber min={1} max={1000} />
            </Form.Item>
            <Form.Item name="min_ready" label="最小预热数量" rules={[{ required: true }]}>
              <InputNumber min={0} max={1000} />
            </Form.Item>
            <Form.Item name="max_concurrency" label="最大并发" rules={[{ required: true }]}>
              <InputNumber min={1} max={1000} />
            </Form.Item>
          </Space>
          <Form.Item name="default_image_id" label="自动补建默认镜像" rules={[{ required: true, message: '请选择默认镜像' }]}>
            <Select
              loading={poolImagesQuery.isFetching || imagesQuery.isFetching}
              options={poolImages.filter((target) => target.enabled).map((target) => {
                const image = imageByID.get(target.image_id)
                return { value: target.image_id, label: image ? `${image.name}（Android API ${image.api_level}）` : target.image_id }
              })}
            />
          </Form.Item>
          <Typography.Paragraph type="secondary">
            总目标是该池最多维持的设备总数，不会把 Android 13～16 的镜像数量相加。自动增加设备只使用默认镜像；切换默认镜像不会重装已有设备。最大并发不能超过总目标，最小预热可以设置为 0；此时平常不保留暖机，但出现可由默认镜像满足的待处理预约时仍会按需创建。
          </Typography.Paragraph>
          {configPool && (
            <Alert
              showIcon
              style={{ marginBottom: 16 }}
              type={currentPoolDevices < configPool.min_ready ? 'warning' : 'info'}
              message={`当前 ${currentPoolDevices} 台 · 总目标 ${configPool.total_target} 台 · 最小预热 ${configPool.min_ready} 台`}
              description={currentPoolDevices < configPool.min_ready
                ? `尚缺 ${configPool.min_ready - currentPoolDevices} 台。系统会按默认镜像自动补建；如果持续不变化，通常是宿主机实际 CPU、内存或 Docker 数据盘不足，可到“宿主机”页面查看实时资源。目标会保留，资源恢复后继续补建。`
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

        <Typography.Title level={5} style={{ marginTop: 24 }}>
          可选镜像
          <Button size="small" style={{ marginLeft: 8 }} loading={poolImagesQuery.isFetching} onClick={() => void queryClient.invalidateQueries({ queryKey: getListDevicePoolImagesQueryKey() })}>
            刷新
          </Button>
        </Typography.Title>
        <Table<DevicePoolImage>
          rowKey="image_id"
          size="small"
          columns={targetColumns}
          dataSource={poolImages}
          pagination={false}
          locale={{ emptyText: '该池暂无可选镜像' }}
        />

        <Typography.Title level={5} style={{ marginTop: 24 }}>设备</Typography.Title>
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
