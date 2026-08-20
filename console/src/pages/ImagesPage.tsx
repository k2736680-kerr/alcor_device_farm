import { useState } from 'react'
import { App as AntApp, Button, Card, Form, Input, Modal, Popconfirm, Segmented, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import {
  getListAndroidSystemImagesQueryKey,
  getListDeviceImagesQueryKey,
  useListAndroidSystemImages,
  useListDeviceImages,
  usePrepareAndroidSystemImage,
  useRetireDeviceImage,
  useSynchronizeAndroidSystemImages,
  useValidateDeviceImage,
} from '../api/generated/device-farm'
import type { AndroidSystemImage, ConsoleRole, DeviceImage, DeviceImageStatus, EmulatorRuntimeProfile } from '../api/generated/models'
import { unwrapData, unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { imageStatusLabel } from '../api/labels'
import { apiErrorText, responseRequestID } from '../api/presentation'
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

interface RetirementValues { reason: string }

export function ImagesPage({ role }: ImagesPageProps) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const validate = useValidateDeviceImage()
  const retire = useRetireDeviceImage()
  const [retirementForm] = Form.useForm<RetirementValues>()
  const [selected, setSelected] = useState<AndroidSystemImage>()
  const [retiringImage, setRetiringImage] = useState<DeviceImage>()
  const [imageView, setImageView] = useState<DeviceImageStatus>('ready')
  // The shared fetcher generates a fresh idempotency key for every write.
  // Keeping it there avoids accidentally reusing one key for distinct form submissions.
  const synchronize = useSynchronizeAndroidSystemImages()
  const prepare = usePrepareAndroidSystemImage()

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceImagesQueryKey() })
    void queryClient.invalidateQueries({ queryKey: getListAndroidSystemImagesQueryKey() })
  }

  const submitRetirement = async () => {
    if (!retiringImage) return
    const values = await retirementForm.validateFields().catch(() => undefined)
    if (!values) return
    retire.mutate({ id: retiringImage.id, data: { reason: values.reason.trim() } }, {
      onSuccess: (data) => {
        message.success(`旧 Android 镜像已停用并移入归档（请求编号：${responseRequestID(data)}）`)
        setRetiringImage(undefined)
        retirementForm.resetFields()
        invalidate()
      },
      onError: (error) => message.error(`停用失败：${apiErrorText(error)}`),
    })
  }

  const synchronizeCatalog = () => synchronize.mutate(undefined, {
    onSuccess: (data) => { message.success(`官方稳定版目录同步已受理（请求编号：${responseRequestID(data)}）`); invalidate() },
    onError: (error) => message.error(`目录同步被拒绝：${apiErrorText(error)}`),
  })

  const submitPreparation = async () => {
    if (!selected) return
    prepare.mutate({ data: { catalog_id: selected.id, runtime_profile: defaultProfile } }, {
      onSuccess: (data) => { message.success(`镜像准备任务已受理，首次下载和构建需要等待（请求编号：${responseRequestID(data)}）`); setSelected(undefined); invalidate() },
      onError: (error) => message.error(`准备任务被拒绝：${apiErrorText(error)}`),
    })
  }

  const runValidate = (image: DeviceImage) => {
    validate.mutate(
      { id: image.id },
      {
        onSuccess: (data) => {
          message.success(`Android 镜像验证已受理（请求编号：${responseRequestID(data)}）`)
          invalidate()
        },
        onError: (error) => {
          message.error(`验证被拒绝：${apiErrorText(error)}`)
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
    { title: '镜像摘要', dataIndex: 'docker_digest', ellipsis: true, render: (value: string) => shortID(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 190,
      fixed: 'right',
      render: (_, image) => role === 'admin' ? (
        <Space size={4}>
          {image.status === 'ready' && <Button size="small" danger onClick={() => {
            retirementForm.resetFields()
            setRetiringImage(image)
          }}>停用</Button>}
          {canValidate(image) && (
          <Popconfirm
            title="发起镜像验证"
            description="将向宿主代理下发镜像验证命令，验证完成前镜像不可用。"
            okText="确认验证"
            onConfirm={() => runValidate(image)}
          >
            <Button size="small" loading={validate.isPending}>验证</Button>
          </Popconfirm>
          )}
        </Space>
      ) : (
          <Typography.Text type="secondary">-</Typography.Text>
      ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const catalogQuery = useListAndroidSystemImages({ query: { refetchInterval: 10_000, refetchOnWindowFocus: true } })
  const catalog = unwrapData<AndroidSystemImage[]>(catalogQuery.data) ?? []
  const { data, isLoading } = useListDeviceImages(
    { page, page_size: pageSize, status: imageView },
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
          版本、镜像类型和 ABI 来自 Android SDK 稳定频道。这里仅负责受控下载、构建和验证；Phone、CPU、内存、磁盘、分辨率和 GPU 在“设备 → 新增 Android 模拟器”中选择。浏览器不会直接访问 Google 服务。
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
            { title: '状态', dataIndex: 'status', render: (value: string) => { const item = catalogStatus[value] ?? { label: '未知状态', color: 'default' }; return <Tag color={item.color}>{item.label}</Tag> } },
            { title: '操作', key: 'action', render: (_, value) => role === 'admin' && !['preparing', 'validating'].includes(value.status)
              ? <Button size="small" onClick={() => setSelected(value)}>{value.status === 'cached' ? '重新准备' : '准备镜像'}</Button>
              : <Typography.Text type="secondary">-</Typography.Text> },
          ]}
        />
      </Card>
      <Card
        title="Android 设备镜像列表"
        extra={<Segmented value={imageView} options={[{ label: '可用镜像', value: 'ready' }, { label: '已停用归档', value: 'disabled' }]}
          onChange={(value) => { setImageView(value as DeviceImageStatus); onPageChange(1, pageSize) }} />}
        styles={{ body: { padding: 0 } }}
      >
        <PageTable<DeviceImage>
          columns={columns} dataSource={result?.items} loading={isLoading} total={result?.total ?? 0}
          page={result?.page ?? page} pageSize={result?.page_size ?? pageSize} onPageChange={onPageChange}
        />
      </Card>
      <Modal
        open={Boolean(selected)} title={selected ? `准备 Android ${selected.api_level - 20} / ${selected.image_type} / ${selected.abi}` : ''}
        okText="提交准备任务" cancelText="取消" confirmLoading={prepare.isPending} onCancel={() => setSelected(undefined)} onOk={() => void submitPreparation()}
      >
        <Typography.Paragraph type="warning">首次使用会从官方源下载并构建，成功验证前不会出现在创建设备的可选列表中。构建验证使用受控默认规格；实际设备规格由创建向导保存。</Typography.Paragraph>
      </Modal>
      <Modal
        open={Boolean(retiringImage)}
        title={retiringImage ? `停用旧镜像 · ${retiringImage.name}` : ''}
        okText="确认停用"
        okButtonProps={{ danger: true }}
        cancelText="取消"
        confirmLoading={retire.isPending}
        onCancel={() => setRetiringImage(undefined)}
        onOk={() => void submitRetirement()}
        destroyOnHidden
      >
        <Typography.Paragraph type="warning">
          仍作为设备池默认值或仍被运行中/隔离中的设备引用时，服务端会拒绝停用。成功后默认列表不再显示，但历史设备和审计记录仍会保留。
        </Typography.Paragraph>
        <Form form={retirementForm} layout="vertical">
          <Form.Item name="reason" label="停用原因（必填并写入审计）" rules={[
            { required: true, whitespace: true, message: '请填写停用原因' },
            { min: 3, message: '停用原因至少填写 3 个字' },
          ]}>
            <Input.TextArea rows={3} maxLength={500} placeholder="例如：旧的验收镜像已不再使用" />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  )
}
