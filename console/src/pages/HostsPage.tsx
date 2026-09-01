import { App as AntApp, Button, Form, Input, Modal, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  getListDeviceHostsQueryKey,
  getDeviceHost,
  useDrainDeviceHost,
  useListDeviceHosts,
  useUpdateDeviceHost,
  useUndrainDeviceHost,
} from '../api/generated/device-farm'
import type { ConsoleRole, DeviceHost } from '../api/generated/models'
import { unwrapData, unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime } from '../api/format'
import { hostStatusLabel, hostTypeLabel } from '../api/labels'
import { apiErrorText, detailText, responseRequestID } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { ReasonActionModal } from '../components/ReasonActionModal'
import { PageQueryError, ResourceDetailDrawer, ResourcePageHeader } from '../components/ResourcePage'

type HostAction = 'drain' | 'undrain'

interface HostNameFormValues {
  name: string
}

interface ActionState {
  host: DeviceHost
  action: HostAction
}

function numeric(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function resourceText(host: DeviceHost, kind: 'cpu' | 'memory' | 'disk') {
  const capacity = host.capacity as Record<string, unknown>
  const used = host.used_capacity as Record<string, unknown>
  if (capacity.resource_model !== 'dynamic_v1') {
    return '等待宿主代理上报'
  }
  if (kind === 'cpu') {
    const total = numeric(capacity.cpu_cores) ?? '-'
    return host.host_os === 'macos' ? `共享使用，共 ${total} 核` : `配额 ${numeric(used.cpu_cores) ?? 0} / ${total} 核`
  }
  if (kind === 'memory') {
    if (host.host_os === 'macos') {
      return `共享使用；实时可用 ${numeric(capacity.memory_available_mb) ?? '-'} / ${numeric(capacity.memory_total_mb) ?? '-'} MB`
    }
    return `配额 ${numeric(used.memory_mb) ?? 0} / ${numeric(capacity.memory_total_mb) ?? '-'} MB；实时可用 ${numeric(capacity.memory_available_mb) ?? '-'} MB`
  }
  return `可用 ${numeric(capacity.disk_available_mb) ?? '-'} / ${numeric(capacity.disk_total_mb) ?? '-'} MB`
}

function capacityPolicy(host: DeviceHost) {
  const capacity = host.capacity as Record<string, unknown>
  if (capacity.resource_model !== 'dynamic_v1') {
    const slots = numeric(capacity.device_slots)
    return slots ? `旧槽位上限 ${slots} 台` : '等待动态资源心跳'
  }
  const slots = numeric(capacity.device_slots)
  return slots ? `按规格动态计算，另设 ${slots} 台安全上限` : '按所选规格和实际剩余资源动态计算'
}

function capabilityObject(host: DeviceHost, key: string): Record<string, unknown> | undefined {
  const value = (host.capabilities as Record<string, unknown>)[key]
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

function hostReadiness(host: DeviceHost) {
  if (host.host_os === 'macos') {
    const readiness = capabilityObject(host, 'host_readiness')
    return readiness?.ready === true ? <Tag color="green">自动化就绪</Tag> : <Tag color="red">自动化未就绪</Tag>
  }
  const capabilities = host.capabilities as Record<string, unknown>
  const ready = host.status === 'online' && capabilities.kvm === true && capabilities.docker === true
  return ready ? <Tag color="green">自动化就绪</Tag> : <Tag color="red">自动化未就绪</Tag>
}

export function HostsPage({ role = 'admin' }: { role?: ConsoleRole }) {
  const { message } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [actionState, setActionState] = useState<ActionState | null>(null)
  const [detailHost, setDetailHost] = useState<DeviceHost | null>(null)
  const [renamingHost, setRenamingHost] = useState<DeviceHost | null>(null)
  const [nameForm] = Form.useForm<HostNameFormValues>()

  const drain = useDrainDeviceHost()
  const undrain = useUndrainDeviceHost()
  const updateHost = useUpdateDeviceHost()

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: getListDeviceHostsQueryKey() })
  }

  const submitAction = (reason: string) => {
    if (!actionState) {
      return
    }
    const { host, action } = actionState
    const mutation = action === 'drain' ? drain : undrain
    mutation.mutate(
      { id: host.id, data: { reason } },
      {
        onSuccess: (data) => {
          message.success(`${action === 'drain' ? '排空' : '解除排空'}已受理（请求编号：${responseRequestID(data)}）`)
          setActionState(null)
          invalidate()
        },
        onError: (error) => {
          message.error(`操作被拒绝：${apiErrorText(error)}`)
        },
      },
    )
  }

  const openRename = (host: DeviceHost) => {
    setRenamingHost(host)
    nameForm.setFieldsValue({ name: host.name })
  }

  const submitRename = async ({ name }: HostNameFormValues) => {
    if (!renamingHost) return
    let latest: DeviceHost
    try {
      const response = await getDeviceHost(renamingHost.id)
      const freshHost = unwrapData<DeviceHost>(response)
      if (!freshHost) throw new Error('未读取到宿主机详情')
      latest = freshHost
    } catch (error) {
      message.error(`更新失败：${apiErrorText(error)}`)
      return
    }
    updateHost.mutate({
      id: latest.id,
      data: {
        name: name.trim(),
        host_type: latest.host_type,
        host_os: latest.host_os,
        host_arch: latest.host_arch,
        address: latest.address,
        capabilities: latest.capabilities,
        capacity: latest.capacity,
      },
    }, {
      onSuccess: (data) => {
        message.success(`宿主机名称已更新（请求编号：${responseRequestID(data)}）`)
        setRenamingHost(null)
        nameForm.resetFields()
        invalidate()
      },
      onError: (error) => message.error(`更新失败：${apiErrorText(error)}`),
    })
  }

  const columns: TableColumnsType<DeviceHost> = [
    {
      title: '宿主机', dataIndex: 'name', width: 240, render: (value: string, host) => (
        <div className="primary-resource">
          <Typography.Text strong>{value}</Typography.Text>
          <small>{host.host_os === 'macos' ? 'macOS / iOS' : 'Linux / Android'} · {host.host_arch}</small>
          <small>{hostTypeLabel(host.host_type)}</small>
        </div>
      ),
    },
    {
      title: '运行与调度', key: 'operation', width: 190, render: (_, host) => (
        <Space size={[4, 4]} wrap>
          <Tag color={host.status === 'online' ? 'green' : host.status === 'draining' ? 'orange' : 'red'}>{hostStatusLabel(host.status)}</Tag>
          {host.draining ? <Tag color="orange">暂停接单</Tag> : <Tag color="blue">接受调度</Tag>}
          {hostReadiness(host)}
        </Space>
      ),
    },
    {
      title: '关键容量', key: 'capacity', width: 300, render: (_, host) => (
        <Space direction="vertical" size={0}>
          <Typography.Text>CPU：<span>{resourceText(host, 'cpu')}</span></Typography.Text>
          <Typography.Text>内存：<span>{resourceText(host, 'memory')}</span></Typography.Text>
          <Typography.Text>磁盘：<span>{resourceText(host, 'disk')}</span></Typography.Text>
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 150,
      fixed: 'right',
      render: (_, host) => (
        <Space size={4} wrap>
          <Button size="small" onClick={() => setDetailHost(host)}>详情</Button>
          {role === 'admin' && <Button size="small" onClick={() => openRename(host)}>改名</Button>}
          {role === 'admin' && <>
          {!host.draining && host.status !== 'maintenance' && (
            <Button size="small" danger onClick={() => setActionState({ host, action: 'drain' })}>排空</Button>
          )}
          {host.draining && (
            <Button size="small" onClick={() => setActionState({ host, action: 'undrain' })}>解除排空</Button>
          )}
          </>}
        </Space>
      ),
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const query = useListDeviceHosts(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DeviceHost>(query.data)

  return (
    <Space direction="vertical" size={14} style={{ display: 'flex' }}>
      <ResourcePageHeader
        title="宿主机"
        description="查看承载 Android 与 iOS 设备的主机是否在线、是否接受调度，以及当前关键资源是否足够。Agent 在线只代表控制链路正常，不等于设备一定可用。"
        dataUpdatedAt={query.dataUpdatedAt}
        isFetching={query.isFetching}
        onRefresh={() => void query.refetch()}
        autoRefreshText="每 10 秒自动更新"
      />
      {query.isError && <PageQueryError error={query.error} onRetry={() => void query.refetch()} />}
      <PageTable<DeviceHost>
        columns={columns}
        dataSource={result?.items}
        loading={query.isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
        locale={{ emptyText: '尚未登记宿主机' }}
      />
      <ResourceDetailDrawer
        open={detailHost !== null}
        title={detailHost ? `宿主机详情 · ${detailHost.name}` : '宿主机详情'}
        onClose={() => setDetailHost(null)}
        items={detailHost ? [
          { key: 'id', label: '完整编号', children: <Typography.Text copyable code>{detailHost.id}</Typography.Text> },
          { key: 'platform', label: '平台与架构', children: `${detailHost.host_os} / ${detailHost.host_arch}` },
          { key: 'type', label: '宿主机类型', children: hostTypeLabel(detailHost.host_type) },
          { key: 'status', label: 'Agent 状态', children: hostStatusLabel(detailHost.status) },
          { key: 'schedule', label: '调度状态', children: detailHost.draining ? '暂停接单（排空中）' : '接受新设备和预约' },
          ...(role === 'admin' ? [{ key: 'address', label: '内部地址', children: detailHost.address ?? '-' }] : []),
          { key: 'cpu', label: 'CPU', children: resourceText(detailHost, 'cpu') },
          { key: 'memory', label: '内存', children: resourceText(detailHost, 'memory') },
          { key: 'disk', label: '数据盘', children: resourceText(detailHost, 'disk') },
          { key: 'policy', label: '容量规则', children: capacityPolicy(detailHost) },
          { key: 'capabilities', label: '能力上报', children: detailText(detailHost.capabilities) },
          { key: 'heartbeat', label: '最后心跳', children: formatTime(detailHost.last_heartbeat_at) },
          { key: 'created', label: '登记时间', children: formatTime(detailHost.created_at) },
        ] : []}
      />
      <ReasonActionModal
        open={actionState !== null}
        title={actionState ? `${actionState.action === 'drain' ? '排空' : '解除排空'} · ${actionState.host.name}` : ''}
        description={actionState?.action === 'drain' ? '排空后不再接受新建设备和新预约；已有预约可以继续运行，结束后可安全维护宿主机。' : '解除排空后宿主机重新接受设备创建和预约调度。'}
        danger={actionState?.action === 'drain'}
        confirmLoading={drain.isPending || undrain.isPending}
        onSubmit={submitAction}
        onCancel={() => setActionState(null)}
      />
      <Modal
        open={renamingHost !== null}
        title={renamingHost ? `修改宿主机名称 · ${renamingHost.name}` : '修改宿主机名称'}
        okText="保存"
        cancelText="取消"
        confirmLoading={updateHost.isPending}
        onOk={() => nameForm.submit()}
        onCancel={() => { setRenamingHost(null); nameForm.resetFields() }}
        destroyOnHidden
      >
        <Form<HostNameFormValues> form={nameForm} layout="vertical" onFinish={submitRename}>
          <Form.Item
            name="name"
            label="宿主机显示名称"
            extra="建议使用“位置/用途 + 系统 + 序号”，例如：上海测试-KVM-01、MacMini-iOS-01。这个名称会出现在设备列表和虚拟机创建页。"
            rules={[
              { required: true, whitespace: true, message: '请输入宿主机名称' },
              { min: 2, max: 40, message: '名称请保持在 2–40 个字符' },
              { pattern: /^[\p{L}\p{N}][\p{L}\p{N} ._\-/]*$/u, message: '可使用中英文、数字、空格、短横线和下划线' },
            ]}
          >
            <Input maxLength={40} showCount placeholder="例如：上海测试-KVM-01" />
          </Form.Item>
          {renamingHost?.address && <Typography.Text type="secondary">服务器地址：{renamingHost.address}</Typography.Text>}
        </Form>
      </Modal>
    </Space>
  )
}
