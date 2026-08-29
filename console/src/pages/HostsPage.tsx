import { App as AntApp, Button, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  getListDeviceHostsQueryKey,
  useDrainDeviceHost,
  useListDeviceHosts,
  useUndrainDeviceHost,
} from '../api/generated/device-farm'
import type { ConsoleRole, DeviceHost } from '../api/generated/models'
import { unwrapPage } from '../api/unwrap'
import { useServerPage } from '../api/useServerPage'
import { formatTime, shortID } from '../api/format'
import { hostStatusLabel, hostTypeLabel } from '../api/labels'
import { apiErrorText, responseRequestID } from '../api/presentation'
import { PageTable } from '../components/PageTable'
import { ReasonActionModal } from '../components/ReasonActionModal'

type HostAction = 'drain' | 'undrain'

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

  const drain = useDrainDeviceHost()
  const undrain = useUndrainDeviceHost()

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

  const columns: TableColumnsType<DeviceHost> = [
    { title: '宿主机编号', dataIndex: 'id', width: 180, render: (value: string) => <Typography.Text code>{shortID(value)}</Typography.Text> },
    { title: '名称', dataIndex: 'name', width: 140 },
    { title: '平台 / 系统', dataIndex: 'host_os', width: 130, render: (value: string) => <Tag color={value === 'macos' ? 'blue' : 'green'}>{value === 'macos' ? 'iOS / macOS' : value === 'linux' ? 'Android / Linux' : value}</Tag> },
    { title: '类型', dataIndex: 'host_type', width: 130, render: (value: string) => hostTypeLabel(value) },
    { title: 'Agent 心跳', dataIndex: 'status', width: 120, render: (value: string) => <Tag color={value === 'online' ? 'green' : value === 'draining' ? 'orange' : 'default'}>{hostStatusLabel(value)}</Tag> },
    { title: '排空', dataIndex: 'draining', width: 80, render: (value: boolean) => (value ? <Tag color="orange">是</Tag> : <Tag>否</Tag>) },
    { title: 'CPU', key: 'cpu', width: 160, render: (_, host) => resourceText(host, 'cpu') },
    { title: '内存', key: 'memory', width: 260, render: (_, host) => resourceText(host, 'memory') },
    { title: '宿主机数据盘', key: 'disk', width: 190, render: (_, host) => resourceText(host, 'disk') },
    { title: '创建规则', key: 'capacity_policy', width: 260, render: (_, host) => capacityPolicy(host) },
    { title: '自动化就绪', key: 'readiness', width: 150, render: (_, host) => hostReadiness(host) },
    { title: '地址', dataIndex: 'address', ellipsis: true, render: (value?: string) => value ?? '-' },
    { title: '最后心跳', dataIndex: 'last_heartbeat_at', width: 160, render: (value?: string) => formatTime(value) },
    { title: '创建时间', dataIndex: 'created_at', width: 160, render: (value: string) => formatTime(value) },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      fixed: 'right',
      render: (_, host) => role === 'admin' ? (
        <Space size={4}>
          {!host.draining && host.status !== 'maintenance' && (
            <Button size="small" danger onClick={() => setActionState({ host, action: 'drain' })}>排空</Button>
          )}
          {host.draining && (
            <Button size="small" onClick={() => setActionState({ host, action: 'undrain' })}>解除排空</Button>
          )}
        </Space>
      ) : <Typography.Text type="secondary">只读</Typography.Text>,
    },
  ]

  const { page, pageSize, onPageChange } = useServerPage()
  const { data, isLoading } = useListDeviceHosts(
    { page, page_size: pageSize },
    { query: { refetchInterval: 10_000, refetchOnWindowFocus: true, refetchOnReconnect: true } },
  )
  const result = unwrapPage<DeviceHost>(data)

  return (
    <>
      <PageTable<DeviceHost>
        columns={columns}
        dataSource={result?.items}
        loading={isLoading}
        total={result?.total ?? 0}
        page={result?.page ?? page}
        pageSize={result?.page_size ?? pageSize}
        onPageChange={onPageChange}
      />
      <ReasonActionModal
        open={actionState !== null}
        title={actionState ? `${actionState.action === 'drain' ? '排空' : '解除排空'} · ${shortID(actionState.host.id)}` : ''}
        description={actionState?.action === 'drain' ? '排空后不再接受新建设备和新预约；已有预约可以继续运行，结束后可安全维护宿主机。' : '解除排空后宿主机重新接受设备创建和预约调度。'}
        danger={actionState?.action === 'drain'}
        confirmLoading={drain.isPending || undrain.isPending}
        onSubmit={submitAction}
        onCancel={() => setActionState(null)}
      />
    </>
  )
}
