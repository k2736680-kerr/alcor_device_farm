import {
  App as AntApp,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
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
  useListDevicePoolImages,
  useListDevicePools,
  useListDevices,
  useSetDevicePoolImageTarget,
  useUpdateDevicePool,
} from '../api/generated/device-farm'
import type { DevicePool, DevicePoolImage, Device } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { PageTable } from '../components/PageTable'

interface PoolFormValues {
  name: string
  default_lease_seconds: number
  max_lease_seconds: number
  max_concurrency: number
}

interface TargetFormValues {
  min_ready: number
  max_instances: number
  enabled: boolean
}

function errorText(error: unknown): string {
  const err = error as { code?: string; requestId?: string; message?: string }
  return `${err.code ?? 'ERROR'}（request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`
}

export function PoolsPage() {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [configPool, setConfigPool] = useState<DevicePool | null>(null)
  const [editTarget, setEditTarget] = useState<DevicePoolImage | null>(null)
  const [addDeviceOpen, setAddDeviceOpen] = useState(false)
  const [selectedDevice, setSelectedDevice] = useState<string | null>(null)
  const [targetForm] = Form.useForm<TargetFormValues>()
  const [poolForm] = Form.useForm<PoolFormValues>()

  const updatePool = useUpdateDevicePool()
  const setTarget = useSetDevicePoolImageTarget()
  const disableTarget = useDisableDevicePoolImageTarget()
  const addDevice = useAddDeviceToPool()

  const invalidatePools = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicePoolsQueryKey() })
  }
  const invalidatePoolImages = () => {
    void queryClient.invalidateQueries({ queryKey: getListDevicePoolImagesQueryKey() })
  }

  const poolImagesQuery = useListDevicePoolImages(
    configPool?.id ?? '',
    { page: 1, page_size: 100 },
    { query: { enabled: Boolean(configPool) } },
  )
  const poolImages = unwrapPage<DevicePoolImage>(poolImagesQuery.data)?.items ?? []
  const devicesQuery = useListDevices({ page: 1, page_size: 200 }, { query: { enabled: addDeviceOpen } })
  const devices = unwrapPage<Device>(devicesQuery.data)?.items ?? []

  const savePool = (values: PoolFormValues) => {
    if (!configPool) {
      return
    }
    updatePool.mutate(
      { id: configPool.id, data: values },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`池配置已更新（request_id: ${requestID}）`)
          invalidatePools()
        },
        onError: (error) => message.error(`更新失败：${errorText(error)}`),
      },
    )
  }

  const saveTarget = (values: TargetFormValues) => {
    if (!configPool || !editTarget) {
      return
    }
    setTarget.mutate(
      { id: configPool.id, imageId: editTarget.image_id, data: values },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`镜像目标已更新（request_id: ${requestID}）`)
          setEditTarget(null)
          invalidatePoolImages()
        },
        onError: (error) => message.error(`更新失败：${errorText(error)}`),
      },
    )
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
    { title: '镜像 ID', dataIndex: 'image_id', width: 190, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '最低就绪', dataIndex: 'min_ready', width: 90 },
    { title: '最大实例', dataIndex: 'max_instances', width: 90 },
    { title: '启用', dataIndex: 'enabled', width: 70, render: (value: boolean) => (value ? <Tag color="green">是</Tag> : <Tag>否</Tag>) },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      render: (_, target) => (
        <Space size={4}>
          <Button
            size="small"
            onClick={() => {
              setEditTarget(target)
              targetForm.setFieldsValue({ min_ready: target.min_ready, max_instances: target.max_instances, enabled: target.enabled })
            }}
          >
            编辑
          </Button>
          {target.enabled && (
            <Popconfirm title="停用该镜像目标？" okText="停用" onConfirm={() => disable(target)}>
              <Button size="small" danger>停用</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  const columns: TableColumnsType<DevicePool> = [
    { title: 'ID', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag> },
    { title: '默认租期(s)', dataIndex: 'default_lease_seconds', width: 120 },
    { title: '最长租期(s)', dataIndex: 'max_lease_seconds', width: 120 },
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
            })
          }}
        >
          配置
        </Button>
      ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDevicePools({ page, page_size: pageSize })
  const result = unwrapPage<DevicePool>(data)

  return (
    <>
      <PageTable<DevicePool>
        columns={columns}
        dataSource={result?.items}
        loading={isFetching}
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
            <Form.Item name="default_lease_seconds" label="默认租期(s)" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400} />
            </Form.Item>
            <Form.Item name="max_lease_seconds" label="最长租期(s)" rules={[{ required: true }]}>
              <InputNumber min={60} max={86400 * 7} />
            </Form.Item>
            <Form.Item name="max_concurrency" label="最大并发" rules={[{ required: true }]}>
              <InputNumber min={1} max={1000} />
            </Form.Item>
          </Space>
          <Button type="primary" loading={updatePool.isPending} onClick={() => poolForm.submit()}>保存基本信息</Button>
        </Form>

        <Typography.Title level={5} style={{ marginTop: 24 }}>
          镜像目标
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
          locale={{ emptyText: '该池暂无镜像目标' }}
        />

        <Typography.Title level={5} style={{ marginTop: 24 }}>设备</Typography.Title>
        <Button size="small" onClick={() => setAddDeviceOpen(true)}>加入设备</Button>
      </Drawer>

      <Modal
        open={editTarget !== null}
        title="编辑镜像目标"
        okText="保存"
        onCancel={() => setEditTarget(null)}
        onOk={() => targetForm.submit()}
        confirmLoading={setTarget.isPending}
        destroyOnClose
      >
        <Form<TargetFormValues> form={targetForm} layout="vertical" onFinish={saveTarget}>
          <Form.Item name="min_ready" label="最低就绪实例数" rules={[{ required: true }]}>
            <InputNumber min={0} max={1000} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="max_instances" label="最大实例数" rules={[{ required: true }]}>
            <InputNumber min={1} max={1000} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

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
        destroyOnClose
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
        label: `${shortID(device.id)} · ${device.serial} · ${device.lifecycle_status}`,
      }))}
    />
  )
}
