type APIErrorLike = { code?: string; requestId?: string; message?: string }

export function apiErrorText(error: unknown): string {
  const value = error as APIErrorLike
  const originalMessage = value.message?.trim() || ''
  const translatedMessages: Record<string, string> = {
    'remote control request timed out': '远控连接请求超时',
  }
  const message = translatedMessages[originalMessage]
    ?? (/[㐀-鿿]/.test(originalMessage) ? originalMessage : '操作未完成，请根据错误代码联系管理员')
  const code = value.code?.trim() || '未知'
  return `${message}（错误代码：${code}；请求编号：${value.requestId ?? '-'}）`
}

export function responseRequestID(response: unknown): string {
  return (response as { request_id?: string } | undefined)?.request_id ?? '-'
}

export function platformLabel(value?: string | null): string {
  if (value === 'android') return 'Android'
  if (value === 'ios') return 'iOS'
  return '未知平台'
}

export function durationLabel(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value) || value < 0) return '-'
  const seconds = Math.floor(value)
  if (seconds < 60) return `${seconds} 秒`
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const parts: string[] = []
  if (days > 0) parts.push(`${days} 天`)
  if (hours > 0) parts.push(`${hours} 小时`)
  if (minutes > 0) parts.push(`${minutes} 分钟`)
  return parts.join(' ') || `${seconds} 秒`
}

const detailKeyLabels: Record<string, string> = {
  appium_session_id: '自动化会话编号',
  base_device_id: '扩容模板设备',
  capabilities: '设备能力',
  catalog_id: '系统目录条目编号',
  command_id: '宿主机命令编号',
  command_type: '宿主机命令类型',
  data_disk_mb: '设备数据盘（MB）',
  device_id: '设备编号',
  device_type_id: 'iPhone 机型标识',
  docker_digest: 'Android 镜像摘要',
  docker_image: 'Android 镜像引用',
  enabled: '是否启用',
  error_code: '错误代码',
  existing_devices_reimaged: '是否重装现有设备',
  history_preserved: '是否保留历史',
  host_id: '宿主机编号',
  image_disk_mb: '共享镜像层（MB）',
  image_id: '镜像编号',
  max_concurrency: '最大并发',
  max_instances: '最大设备数',
  min_ready: '最小可用数',
  platform: '平台',
  provisioning_key: '创建任务标识',
  pool_id: '设备池编号',
  pool_links_disabled: '是否停用设备池关联',
  pool_total_target: '设备池目标数',
  previous_status: '原状态',
  provider_ref: '运行组件标识',
  reservation_id: '预约编号',
  rollback_restored: '是否已恢复旧配置',
  runtime_profile: '运行规格',
  runtime_id: 'iOS 运行时标识',
  session_id: '设备会话编号',
  source_address: '来源地址',
  status: '当前状态',
  target_image_id: '目标镜像编号',
  target_instances: '目标设备数',
  total_target: '目标设备数',
}

function detailValue(value: unknown): string {
  if (value === null || value === undefined || value === '') return '-'
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (value === 'android' || value === 'ios') return platformLabel(value)
  if (Array.isArray(value)) return value.map(detailValue).join('、') || '-'
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
    return entries.map(([key, item]) => `${detailKeyLabels[key] ?? `字段 ${key}`}：${detailValue(item)}`).join('，') || '-'
  }
  return String(value)
}

export function detailText(value?: Record<string, unknown> | null): string {
  if (!value || Object.keys(value).length === 0) return '-'
  return Object.entries(value)
    .map(([key, item]) => `${detailKeyLabels[key] ?? `事件字段 ${key}`}：${detailValue(item)}`)
    .join('；')
}
