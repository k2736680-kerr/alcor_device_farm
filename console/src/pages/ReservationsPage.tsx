import {
  App as AntApp,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import type { FormInstance, TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  getListDeviceReservationsQueryKey,
  useCreateDeviceReservation,
  useExtendDeviceReservation,
  useListDevicePools,
  useListDeviceReservations,
  useReleaseDeviceReservation,
} from '../api/generated/device-farm'
import type { DevicePool, Reservation } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { ownerTypeLabel, poolStatusLabel, reservationStatusLabel } from '../api/labels'
import { PageTable } from '../components/PageTable'
import { ReasonActionModal } from '../components/ReasonActionModal'

const statusColor: Record<string, string> = {
  active: 'green',
  pending: 'orange',
  released: 'default',
  expired: 'default',
  failed: 'red',
  force_released: 'purple',
}

interface CreateFormValues {
  pool_id: string
  lease_seconds: number
}

interface ExtendFormValues {
  additional_seconds: number
}

function errorText(error: unknown): string {
  const err = error as { code?: string; requestId?: string; message?: string }
  return `${err.code ?? 'ERROR'}（request_id: ${err.requestId ?? '-'}）：${err.message ?? ''}`
}

export function ReservationsPage() {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [extendFor, setExtendFor] = useState<Reservation | null>(null)
  const [releaseFor, setReleaseFor] = useState<Reservation | null>(null)
  const [createForm] = useCreateForm()
  const [extendForm] = useExtendForm()

  const create = useCreateDeviceReservation()
  const extend = useExtendDeviceReservation()
  const release = useReleaseDeviceReservation()

  const poolsQuery = useListDevicePools({ page: 1, page_size: 100 })
  const pools = unwrapPage<DevicePool>(poolsQuery.data)?.items ?? []

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceReservationsQueryKey() })
  }

  const submitCreate = (values: CreateFormValues) => {
    create.mutate(
      { data: { pool_id: values.pool_id, owner_type: 'manual', owner_id: '', lease_seconds: values.lease_seconds } },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`预约已创建（request_id: ${requestID}），等待分配设备…`)
          createForm.resetFields()
          setCreateOpen(false)
          invalidate()
        },
        onError: (error) => message.error(`创建失败：${errorText(error)}`),
      },
    )
  }

  const submitExtend = (values: ExtendFormValues) => {
    if (!extendFor) {
      return
    }
    extend.mutate(
      { id: extendFor.id, data: values },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`租期已续（request_id: ${requestID}）`)
          setExtendFor(null)
          invalidate()
        },
        onError: (error) => message.error(`续租失败：${errorText(error)}`),
      },
    )
  }

  const submitRelease = (reason: string) => {
    if (!releaseFor) {
      return
    }
    release.mutate(
      { id: releaseFor.id, data: { reason } },
      {
        onSuccess: (data) => {
          const requestID = (data as { request_id?: string } | undefined)?.request_id ?? '-'
          message.success(`${releaseFor.status === 'pending' ? '预约已取消' : '预约已释放'}（request_id: ${requestID}）`)
          setReleaseFor(null)
          invalidate()
        },
        onError: (error) => message.error(`释放失败：${errorText(error)}`),
      },
    )
  }

  const columns: TableColumnsType<Reservation> = [
    { title: '预约编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '状态', dataIndex: 'status', width: 110, render: (value: string) => <Tag color={statusColor[value] ?? 'default'}>{reservationStatusLabel(value)}</Tag> },
    { title: '预约类型', dataIndex: 'owner_type', width: 110, render: (value: string) => ownerTypeLabel(value) },
    { title: '预约归属', dataIndex: 'owner_id', width: 170, render: (value: string) => shortID(value) },
    { title: '设备池', dataIndex: 'pool_id', width: 150, render: (value: string) => shortID(value) },
    { title: '设备', dataIndex: 'device_id', width: 150, render: (value?: string) => (value ? shortID(value) : '-') },
    { title: '租期（秒）', dataIndex: 'lease_seconds', width: 100 },
    { title: '开始', dataIndex: 'starts_at', width: 160, render: (value?: string) => formatTime(value) },
    { title: '到期', dataIndex: 'expires_at', width: 160, render: (value?: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      fixed: 'right',
      render: (_, reservation) => (
        <Space size={4} wrap>
          {reservation.status === 'active' && (
            <Button size="small" onClick={() => setExtendFor(reservation)}>续租</Button>
          )}
          {(reservation.status === 'pending' || reservation.status === 'active') && (
            <Button size="small" danger onClick={() => setReleaseFor(reservation)}>
              {reservation.status === 'pending' ? '取消' : '释放'}
            </Button>
          )}
        </Space>
      ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isFetching } = useListDeviceReservations({ page, page_size: pageSize }, { query: { refetchInterval: 5000 } })
  const result = unwrapPage<Reservation>(data)

  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => setCreateOpen(true)}>创建人工预约</Button>
        <Typography.Text type="secondary">列表每 5 秒自动刷新，分配成功后状态会变为“使用中”。</Typography.Text>
      </Space>
      <PageTable<Reservation>
        columns={columns}
        dataSource={result?.items}
        loading={isFetching}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
      />

      <Modal
        open={createOpen}
        title="创建人工预约"
        okText="创建"
        cancelText="取消"
        confirmLoading={create.isPending}
        onCancel={() => {
          createForm.resetFields()
          setCreateOpen(false)
        }}
        onOk={() => createForm.submit()}
        destroyOnHidden
      >
        <FormValues form={createForm} onSubmit={submitCreate} pools={pools} poolsLoading={poolsQuery.isFetching} />
      </Modal>

      <Modal
        open={extendFor !== null}
        title="续租"
        okText="续租"
        cancelText="取消"
        confirmLoading={extend.isPending}
        onCancel={() => setExtendFor(null)}
        onOk={() => extendForm.submit()}
        destroyOnHidden
      >
        <Form<ExtendFormValues> form={extendForm} layout="vertical" onFinish={submitExtend}>
          <Form.Item name="additional_seconds" label="续租时长(s)" rules={[{ required: true, message: '请输入续租时长' }]}>
            <InputNumber min={60} max={86400} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <ReasonActionModal
        open={releaseFor !== null}
        title={releaseFor ? `${releaseFor.status === 'pending' ? '取消' : '释放'}预约 · ${shortID(releaseFor.id)}` : ''}
        description={releaseFor?.status === 'pending' ? '取消后不再等待设备，操作会写入审计。' : '释放后设备立即进入清理流程，危险操作。'}
        danger
        confirmLoading={release.isPending}
        onSubmit={submitRelease}
        onCancel={() => setReleaseFor(null)}
      />

    </>
  )
}

// Small helpers to keep the main component readable.
function useCreateForm() {
  const [form] = Form.useForm<CreateFormValues>()
  return [form]
}

function useExtendForm() {
  const [form] = Form.useForm<ExtendFormValues>()
  return [form]
}

function FormValues({
  form,
  onSubmit,
  pools,
  poolsLoading,
}: {
  form: FormInstance<CreateFormValues>
  onSubmit: (values: CreateFormValues) => void
  pools: { id: string; name: string; status: string }[]
  poolsLoading: boolean
}) {
  return (
    <Form<CreateFormValues> form={form} layout="vertical" onFinish={onSubmit}>
      <Form.Item name="pool_id" label="设备池" rules={[{ required: true, message: '请选择设备池' }]}>
        <Select
          loading={poolsLoading}
          placeholder="选择设备池"
          options={pools.map((pool) => ({ value: pool.id, label: `${pool.name} · ${poolStatusLabel(pool.status)}` }))}
        />
      </Form.Item>
      <Form.Item label="预约所有者">
        <Input aria-label="预约所有者" value="由当前登录会话确定，浏览器不可修改" disabled />
      </Form.Item>
      <Form.Item name="lease_seconds" label="租期（秒）" initialValue={1800} rules={[{ required: true, message: '请输入租期' }]}>
        <InputNumber min={60} max={86400} style={{ width: '100%' }} />
      </Form.Item>
    </Form>
  )
}
