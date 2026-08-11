import { useState } from 'react'
import { App as AntApp, Button, Card, Form, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import {
  getListAndroidSystemImagesQueryKey,
  getListDeviceImagesQueryKey,
  useListAndroidSystemImages,
  useListDeviceImages,
  usePrepareAndroidSystemImage,
  useSynchronizeAndroidSystemImages,
  useValidateDeviceImage,
} from '../api/generated/device-farm'
import type { AndroidSystemImage, ConsoleRole, DeviceImage, EmulatorRuntimeProfile } from '../api/generated/models'
import { unwrapData, unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { imageStatusLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'

function canValidate(image: DeviceImage): boolean {
  return image.status === 'draft' || image.status === 'failed' || image.status === 'disabled'
}

function diskSize(value?: number): string {
  if (value === undefined || value <= 0) return '-'
  return value >= 1024 ? `${(value / 1024).toFixed(value % 1024 === 0 ? 0 : 1)} GiB` : `${value} MiB`
}

const defaultProfile: EmulatorRuntimeProfile = {
  container_cpu_cores: 4, container_memory_mb: 5120, guest_cpu_cores: 4, guest_memory_mb: 4096,
  data_disk_mb: 4096, image_disk_mb: 0, width: 1080, height: 2400, density_dpi: 420, vm_heap_mb: 512, graphics: 'auto',
}

const catalogStatus: Record<string, { label: string, color: string }> = {
  downloadable: { label: '可下载', color: 'default' }, preparing: { label: '下载/构建中', color: 'processing' },
  validating: { label: '验证中', color: 'warning' }, cached: { label: '已缓存可用', color: 'success' },
  failed: { label: '准备失败', color: 'error' }, official_updated: { label: '官方已更新', color: 'gold' },
}

interface ImagesPageProps { role: ConsoleRole }

export function ImagesPage({ role }: ImagesPageProps) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const validate = useValidateDeviceImage()
  const [form] = Form.useForm<EmulatorRuntimeProfile>()
  const [selected, setSelected] = useState<AndroidSystemImage>()
  // The shared fetcher generates a fresh idempotency key for every write.
  // Keeping it there avoids accidentally reusing one key for distinct form submissions.
  const synchronize = useSynchronizeAndroidSystemImages()
  const prepare = usePrepareAndroidSystemImage()

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceImagesQueryKey() })
    void queryClient.invalidateQueries({ queryKey: getListAndroidSystemImagesQueryKey() })
  }

  const synchronizeCatalog = () => synchronize.mutate(undefined, {
    onSuccess: () => { message.success('官方稳定版目录同步已受理'); invalidate() },
    onError: (error) => message.error(`目录同步被拒绝：${(error as { message?: string }).message ?? '未知错误'}`),
  })

  const submitPreparation = async () => {
    if (!selected) return
    const runtime_profile = await form.validateFields()
    prepare.mutate({ data: { catalog_id: selected.id, runtime_profile } }, {
      onSuccess: () => { message.success('镜像准备任务已受理，首次下载和构建需要等待'); setSelected(undefined); invalidate() },
      onError: (error) => message.error(`准备任务被拒绝：${(error as { message?: string }).message ?? '未知错误'}`),
    })
  }

  const runValidate = (image: DeviceImage) => {
    validate.mutate(
      { id: image.id },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`镜像验证已受理（request_id: ${requestID}）`)
          invalidate()
        },
        onError: (error) => {
          const err = error as { code?: string; requestId?: string; message?: string }
          message.error(`验证被拒绝（${err.code ?? 'ERROR'}，request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`)
        },
      },
    )
  }

  const columns: TableColumnsType<DeviceImage> = [
    { title: '镜像编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 160 },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={value === 'ready' ? 'green' : value === 'failed' ? 'red' : 'orange'}>{imageStatusLabel(value)}</Tag> },
    { title: 'API 级别', dataIndex: 'api_level', width: 90 },
    { title: 'ABI', dataIndex: 'abi', width: 90 },
    { title: '分辨率', dataIndex: 'resolution', width: 110 },
    { title: '共享镜像层', key: 'image_disk', width: 120, render: (_, image) => diskSize(image.resource_config?.image_disk_mb) },
    { title: '设备数据卷', key: 'data_disk', width: 120, render: (_, image) => diskSize(image.resource_config?.data_disk_mb) },
    { title: '镜像引用', dataIndex: 'docker_image', ellipsis: true, render: (value?: string) => value ?? '-' },
    { title: '摘要', dataIndex: 'docker_digest', ellipsis: true, render: (value: string) => shortID(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      fixed: 'right',
      render: (_, image) =>
        role === 'admin' && canValidate(image) ? (
          <Popconfirm
            title="发起镜像验证"
            description="将向宿主代理下发镜像验证命令，验证完成前镜像不可用。"
            okText="确认验证"
            onConfirm={() => runValidate(image)}
          >
            <Button size="small" loading={validate.isPending}>验证</Button>
          </Popconfirm>
        ) : (
          <Typography.Text type="secondary">-</Typography.Text>
        ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const catalogQuery = useListAndroidSystemImages({ query: { refetchInterval: 10_000, refetchOnWindowFocus: true } })
  const catalog = unwrapData<AndroidSystemImage[]>(catalogQuery.data) ?? []
  const { data, isLoading } = useListDeviceImages(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DeviceImage>(data)
  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Card
        title="Android 官方系统目录"
        extra={role === 'admin' ? <Button loading={synchronize.isPending} onClick={synchronizeCatalog}>同步官方目录</Button> : undefined}
      >
        <Typography.Paragraph type="secondary">
          版本、镜像类型和 ABI 来自 Android SDK 稳定频道；CPU、内存、磁盘、分辨率和 GPU 是本设备农场的运行规格。浏览器不会访问 Google。
        </Typography.Paragraph>
        <Table<AndroidSystemImage>
          rowKey="id" size="small" loading={catalogQuery.isLoading} dataSource={catalog} pagination={false}
          locale={{ emptyText: '尚未同步官方目录，请由管理员发起同步' }}
          columns={[
            { title: 'Android / API', key: 'api', render: (_, value) => (
              <Space size={6}>
                <span>{`Android ${value.api_level - 20} / API ${value.api_level}`}</span>
                {value.api_level === 36 && <Tag color="blue">默认候选</Tag>}
              </Space>
            ) },
            { title: '镜像类型', dataIndex: 'image_type', render: (value) => value === 'google_play' ? 'Google Play' : 'Google APIs' },
            { title: 'ABI', dataIndex: 'abi' },
            { title: '官方修订', dataIndex: 'revision' },
            { title: '状态', dataIndex: 'status', render: (value: string) => { const item = catalogStatus[value] ?? { label: value, color: 'default' }; return <Tag color={item.color}>{item.label}</Tag> } },
            { title: '操作', key: 'action', render: (_, value) => role === 'admin' && !['preparing', 'validating'].includes(value.status)
              ? <Button size="small" onClick={() => { form.setFieldsValue(defaultProfile); setSelected(value) }}>{value.status === 'cached' ? '重新准备' : '准备镜像'}</Button>
              : <Typography.Text type="secondary">-</Typography.Text> },
          ]}
        />
      </Card>
      <Card title="已构建并验证的设备镜像" styles={{ body: { padding: 0 } }}>
        <PageTable<DeviceImage>
          columns={columns} dataSource={result?.items} loading={isLoading} total={result?.total ?? 0}
          page={result?.page ?? page} pageSize={result?.page_size ?? pageSize} onPageChange={onPageChange}
        />
      </Card>
      <Modal
        open={Boolean(selected)} title={selected ? `准备 Android ${selected.api_level - 20} / ${selected.image_type} / ${selected.abi}` : ''}
        okText="提交准备任务" cancelText="取消" confirmLoading={prepare.isPending} onCancel={() => setSelected(undefined)} onOk={() => void submitPreparation()}
      >
        <Typography.Paragraph type="warning">首次使用会从官方源下载并构建，成功验证前不会出现在可用设备镜像中。</Typography.Paragraph>
        <Form form={form} layout="vertical" initialValues={defaultProfile}>
          <Space wrap align="start">
            <Form.Item name="container_cpu_cores" label="容器 CPU（核）" rules={[{ required: true }]}><InputNumber min={1} max={64} /></Form.Item>
            <Form.Item name="container_memory_mb" label="容器内存（MiB）" rules={[{ required: true }]}><InputNumber min={2048} max={262144} step={1024} /></Form.Item>
            <Form.Item name="guest_cpu_cores" label="Android CPU（核）" rules={[{ required: true }]}><InputNumber min={1} max={32} /></Form.Item>
            <Form.Item name="guest_memory_mb" label="Android 内存（MiB）" rules={[{ required: true }]}><InputNumber min={1536} step={512} /></Form.Item>
            <Form.Item name="data_disk_mb" label="设备数据盘（MiB）" rules={[{ required: true }]}><InputNumber min={2048} step={1024} /></Form.Item>
            <Form.Item name="width" label="宽度" rules={[{ required: true }]}><InputNumber min={320} /></Form.Item>
            <Form.Item name="height" label="高度" rules={[{ required: true }]}><InputNumber min={480} /></Form.Item>
            <Form.Item name="density_dpi" label="DPI" rules={[{ required: true }]}><InputNumber min={120} max={960} /></Form.Item>
            <Form.Item name="vm_heap_mb" label="VM Heap（MiB）" rules={[{ required: true }]}><InputNumber min={128} /></Form.Item>
            <Form.Item name="graphics" label="GPU 模式" rules={[{ required: true }]}><Select style={{ width: 140 }} options={[{ value: 'auto', label: '自动' }, { value: 'host', label: '宿主机 GPU' }, { value: 'software', label: '软件渲染' }]} /></Form.Item>
          </Space>
        </Form>
      </Modal>
    </Space>
  )
}
