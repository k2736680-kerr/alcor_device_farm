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
import { useMemo, useState } from 'react'
import {
  getListDeviceReservationsQueryKey,
  useCreateDeviceReservation,
  useExtendDeviceReservation,
  useListDevicePools,
  useListDeviceReservations,
  useReleaseDeviceReservation,
} from '../api/generated/device-farm'
import type { ConsoleRole, DevicePool, Reservation } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { ownerTypeLabel, poolStatusLabel, reservationFailureLabel, reservationStatusLabel } from '../api/labels'
import { apiErrorText, durationLabel, platformLabel, responseRequestID } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { ReasonActionModal } from '../components/ReasonActionModal'
import { PageQueryError, ResourceDetailDrawer, ResourcePageHeader } from '../components/ResourcePage'

const statusColor: Record<string, string> = {
  active: 'green',
  pending: 'orange',
  released: 'default',
  expired: 'default',
  failed: 'red',
  force_released: 'purple',
}

function reservationProgress(reservation: Reservation): string {
  if (reservation.status === 'pending') return '等待设备分配'
  if (reservation.status === 'active' && reservation.expires_at) {
    const seconds = Math.max(0, Math.floor((new Date(reservation.expires_at).getTime() - Date.now()) / 1000))
    return seconds > 0 ? `剩余 ${durationLabel(seconds)}` : '租期已到，等待回收'
  }
  if (reservation.status === 'failed') return `失败：${reservationFailureLabel(reservation.failure_code)}`
  return reservation.released_at ? `结束于 ${formatTime(reservation.released_at)}` : reservationStatusLabel(reservation.status)
}

interface CreateFormValues {
  pool_id: string
  lease_seconds: number
}

interface ExtendFormValues {
  additional_seconds: number
}

export function ReservationsPage({ role = 'admin' }: { role?: ConsoleRole }) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [extendFor, setExtendFor] = useState<Reservation | null>(null)
  const [releaseFor, setReleaseFor] = useState<Reservation | null>(null)
  const [detailReservation, setDetailReservation] = useState<Reservation | null>(null)
  const [createForm] = useCreateForm()
  const [extendForm] = useExtendForm()

  const create = useCreateDeviceReservation()
  const extend = useExtendDeviceReservation()
  const release = useReleaseDeviceReservation()

  const poolsQuery = useListDevicePools({ page: 1, page_size: 100 })
  const pools = unwrapPage<DevicePool>(poolsQuery.data)?.items ?? []
  const poolByID = useMemo(() => new Map(pools.map((pool) => [pool.id, pool])), [pools])

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceReservationsQueryKey() })
  }

  const submitCreate = (values: CreateFormValues) => {
    create.mutate(
      { data: { pool_id: values.pool_id, owner_type: 'manual', owner_id: '', lease_seconds: values.lease_seconds } },
      {
        onSuccess: (data) => {
          message.success(`预约已创建，正在等待分配设备（请求编号：${responseRequestID(data)}）`)
          createForm.resetFields()
          setCreateOpen(false)
          invalidate()
        },
        onError: (error) => message.error(`创建失败：${apiErrorText(error)}`),
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
          message.success(`租期窗口已延长（请求编号：${responseRequestID(data)}）`)
          setExtendFor(null)
          invalidate()
        },
        onError: (error) => message.error(`续租失败：${apiErrorText(error)}`),
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
          message.success(`${releaseFor.status === 'pending' ? '预约已取消' : '预约已释放'}（请求编号：${responseRequestID(data)}）`)
          setReleaseFor(null)
          invalidate()
        },
        onError: (error) => message.error(`释放失败：${apiErrorText(error)}`),
      },
    )
  }

  const columns: TableColumnsType<Reservation> = [
    {
      title: '预约', dataIndex: 'id', width: 200, render: (value: string, reservation) => (
        <div className="primary-resource">
          <Typography.Text code>{shortID(value)}</Typography.Text>
          <small>{ownerTypeLabel(reservation.owner_type)} · {shortID(reservation.owner_id)}</small>
        </div>
      ),
    },
    {
      title: '当前状态', dataIndex: 'status', width: 190, render: (value: string, reservation) => (
        <Space direction="vertical" size={2}>
          <Tag color={statusColor[value] ?? 'default'}>{reservationStatusLabel(value)}</Tag>
          <Typography.Text className={['pending', 'active'].includes(value) ? 'remaining-time' : 'record-finished'}>
            {reservationProgress(reservation)}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '设备池', dataIndex: 'pool_id', width: 200, render: (value: string) => {
        const pool = poolByID.get(value)
        return (
          <div className="primary-resource">
            <Typography.Text>{pool?.name ?? shortID(value)}</Typography.Text>
            <small>{platformLabel(pool?.platform)}</small>
          </div>
        )
      },
    },
    { title: '分配设备', dataIndex: 'device_id', width: 150, render: (value?: string) => (value ? <Typography.Text code>{shortID(value)}</Typography.Text> : <Typography.Text type="secondary">尚未分配</Typography.Text>) },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      fixed: 'right',
      render: (_, reservation) => (
        <Space size={4} wrap>
          <Button size="small" onClick={() => setDetailReservation(reservation)}>详情</Button>
          {role !== 'viewer' && reservation.status === 'active' && (
            <Button size="small" onClick={() => setExtendFor(reservation)}>续租</Button>
          )}
          {role !== 'viewer' && (reservation.status === 'pending' || reservation.status === 'active') && (
            <Button size="small" danger onClick={() => setReleaseFor(reservation)}>
              {reservation.status === 'pending' ? '取消' : '释放'}
            </Button>
          )}
          {!['pending', 'active'].includes(reservation.status) && <span className="record-finished">已结束</span>}
        </Space>
      ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const query = useListDeviceReservations(
    { page, page_size: pageSize },
    { query: { refetchInterval: 5_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<Reservation>(query.data)

  return (
    <Space direction="vertical" size={14} style={{ display: 'flex' }}>
      <ResourcePageHeader
        title="设备预约"
        description="查看设备分配、剩余租期和结束结果。进行中的预约可直接续租或释放，历史记录按最新时间保留用于追溯。"
        actions={role !== 'viewer' ? <Button type="primary" onClick={() => setCreateOpen(true)}>创建人工预约</Button> : undefined}
        dataUpdatedAt={query.dataUpdatedAt}
        isFetching={query.isFetching}
        onRefresh={() => void query.refetch()}
        autoRefreshText="每 5 秒自动更新"
      />
      {query.isError && <PageQueryError error={query.error} onRetry={() => void query.refetch()} />}
      {poolsQuery.isError && <PageQueryError error={poolsQuery.error} onRetry={() => void poolsQuery.refetch()} />}
      <PageTable<Reservation>
        columns={columns}
        dataSource={result?.items}
        loading={query.isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: '暂无预约记录' }}
      />

      <ResourceDetailDrawer
        open={detailReservation !== null}
        title={detailReservation ? `预约详情 · ${shortID(detailReservation.id)}` : '预约详情'}
        onClose={() => setDetailReservation(null)}
        items={detailReservation ? [
          { key: 'id', label: '完整预约编号', children: <Typography.Text code copyable>{detailReservation.id}</Typography.Text> },
          { key: 'status', label: '状态', children: reservationProgress(detailReservation) },
          { key: 'pool', label: '设备池', children: poolByID.get(detailReservation.pool_id)?.name ?? detailReservation.pool_id },
          { key: 'device', label: '设备编号', children: detailReservation.device_id ? <Typography.Text code copyable>{detailReservation.device_id}</Typography.Text> : '尚未分配' },
          { key: 'owner', label: '预约归属', children: `${ownerTypeLabel(detailReservation.owner_type)} · ${detailReservation.owner_id || '-'}` },
          { key: 'lease', label: '初始租期', children: durationLabel(detailReservation.lease_seconds) },
          { key: 'starts', label: '开始时间', children: formatTime(detailReservation.starts_at) },
          { key: 'expires', label: '到期时间', children: formatTime(detailReservation.expires_at) },
          { key: 'released', label: '释放时间', children: formatTime(detailReservation.released_at) },
          { key: 'failure', label: '失败代码', children: detailReservation.failure_code ?? '-' },
          { key: 'created', label: '创建时间', children: formatTime(detailReservation.created_at) },
          { key: 'updated', label: '最后变更', children: formatTime(detailReservation.updated_at) },
        ] : []}
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
          <Typography.Paragraph type="secondary">续租会把当前到期时间向后顺延；这是可继续滑动的租期窗口，不是设备运行总时长上限。</Typography.Paragraph>
          <Form.Item name="additional_seconds" label="续租时长（秒）" rules={[{ required: true, message: '请输入续租时长' }]}>
            <InputNumber min={60} max={86400} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <ReasonActionModal
        open={releaseFor !== null}
        title={releaseFor ? `${releaseFor.status === 'pending' ? '取消' : '释放'}预约 · ${shortID(releaseFor.id)}` : ''}
        description={releaseFor?.status === 'pending' ? '取消后不再等待设备，操作会写入审计。' : '释放后预约结束；健康设备返回可用状态。设备数据不会因为释放预约而自动清空。'}
        danger={releaseFor?.status === 'pending'}
        confirmLoading={release.isPending}
        onSubmit={submitRelease}
        onCancel={() => setReleaseFor(null)}
      />

    </Space>
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
  pools: { id: string; name: string; status: string; platform?: string; default_lease_seconds?: number; max_lease_seconds?: number }[]
  poolsLoading: boolean
}) {
  const selectedPoolID = Form.useWatch('pool_id', form)
  const selectablePools = pools.filter((pool) => pool.status === 'active')
  const selectedPool = selectablePools.find((pool) => pool.id === selectedPoolID)
  return (
    <Form<CreateFormValues> form={form} layout="vertical" onFinish={onSubmit}>
      <Form.Item name="pool_id" label="设备池" rules={[{ required: true, message: '请选择设备池' }]}>
        <Select
          loading={poolsLoading}
          placeholder="选择设备池"
          options={selectablePools.map((pool) => ({ value: pool.id, label: `${platformLabel(pool.platform)} · ${pool.name} · ${poolStatusLabel(pool.status)}` }))}
          onChange={(poolID) => {
            const pool = selectablePools.find((item) => item.id === poolID)
            if (pool?.default_lease_seconds) form.setFieldValue('lease_seconds', pool.default_lease_seconds)
          }}
        />
      </Form.Item>
      <Form.Item label="预约所有者">
        <Input aria-label="预约所有者" value="由当前登录会话确定，浏览器不可修改" disabled />
      </Form.Item>
      <Form.Item
        name="lease_seconds"
        label="初始租期（秒）"
        initialValue={1800}
        extra={selectedPool ? `设备池默认 ${durationLabel(selectedPool.default_lease_seconds)}，单次租期窗口最长 ${durationLabel(selectedPool.max_lease_seconds)}。运行中的自动化可在到期前继续续租。` : '选择设备池后会自动填入该池默认租期。'}
        rules={[{ required: true, message: '请输入租期' }]}
      >
        <InputNumber min={60} max={selectedPool?.max_lease_seconds ?? 86400} style={{ width: '100%' }} />
      </Form.Item>
    </Form>
  )
}
