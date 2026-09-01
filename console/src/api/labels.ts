function lookup(labels: Record<string, string>, value?: string | null): string {
  if (!value) {
    return '-'
  }
  return labels[value] ?? `未识别（${value}）`
}

const deviceKindLabels: Record<string, string> = {
  emulator: 'Android 模拟器',
  simulator: 'iOS 模拟器',
  physical: '真机',
}

const providerTypeLabels: Record<string, string> = {
  docker_emulator: 'Android Docker 模拟器',
  usb_android: 'Android USB 真机',
  appium_device_farm_ios: 'iOS CoreSimulator',
  mock: '模拟测试设备',
}

const lifecycleStatusLabels: Record<string, string> = {
  provisioning: '创建中',
  booting: '启动中',
  ready: '可用',
  reserved: '已预留',
  busy: '使用中',
  recycling: '回收中',
  stopped: '已停止',
  quarantined: '已隔离',
  deleted: '已删除',
}

const healthStatusLabels: Record<string, string> = {
  unknown: '待检测',
  healthy: '正常',
  degraded: '部分异常',
  unhealthy: '故障',
}

const lifecycleModeLabels: Record<string, string> = {
  rebuild: '受控重建',
  clean: '清理数据',
  factory_reset: '恢复出厂',
}

const imageStatusLabels: Record<string, string> = {
  draft: '草稿',
  validating: '验证中',
  ready: '可用',
  failed: '验证失败',
  disabled: '已停用',
}

const hostTypeLabels: Record<string, string> = {
  docker_emulator: 'Android 模拟器宿主机',
  usb_android: 'Android 真机宿主机',
  hybrid: '混合宿主机',
  appium_device_farm_ios: 'iOS 模拟器宿主机',
}

const hostStatusLabels: Record<string, string> = {
  online: '在线',
  offline: '离线',
  draining: '排空中',
  maintenance: '维护中',
}

const poolStatusLabels: Record<string, string> = {
  active: '启用',
  disabled: '停用',
}

const reservationStatusLabels: Record<string, string> = {
  pending: '等待设备',
  active: '使用中',
  released: '已释放',
  failed: '失败',
  expired: '已过期',
  force_released: '已强制释放',
}

const ownerTypeLabels: Record<string, string> = {
  manual: '人工预约',
  run_attempt: '自动化执行',
  test_run: '联调执行',
}

const roleLabels: Record<string, string> = {
  viewer: '只读用户',
  operator: '操作员',
  admin: '管理员',
}

const actorTypeLabels: Record<string, string> = {
  console: '控制台用户',
  service: '外部服务',
  agent: '宿主代理',
  system: '系统',
}

const severityLabels: Record<string, string> = {
  info: '提示',
  warning: '警告',
  error: '错误',
  critical: '严重',
}

const healthSourceLabels: Record<string, string> = {
  reconciler: '状态校准器',
  scheduler: '设备调度器',
  reaper: '过期回收器',
  agent: '宿主代理',
  provider: '设备运行组件',
  stf: '远控服务',
  appium: '自动化服务',
  system: '系统',
}

const healthEventTypeLabels: Record<string, string> = {
  adopted: '设备已纳管',
  health_check_failed: '健康检查失败',
  device_rebuild_failed: '设备重建失败',
  device_management_operation_succeeded: '设备操作成功',
  device_management_operation_failed: '设备操作失败',
  warm_pool_create_failed: '自动创建设备失败',
  warm_pool_scale_down_completed: '自动缩容完成',
  warm_pool_scale_down_failed: '自动缩容失败',
  ios_session_drift_detected: 'iOS 会话状态漂移',
  ios_session_drift: 'iOS 会话状态漂移',
  ios_session_cleanup_failed: 'iOS 会话清理失败',
  ios_provider_device_missing: 'iOS 模拟器已从宿主机清单消失',
}

const auditActionLabels: Record<string, string> = {
  'console.login': '登录控制台',
  retire_device_image: '停用 Android 镜像',
  update_device_pool_capacity: '更新设备池容量',
  set_device_pool_target: '调整目标设备数',
  select_pool_default_image: '选择 Android 默认镜像',
  set_pool_base_device: '设置扩容模板',
  quarantine_device: '隔离设备',
  unquarantine_device: '解除设备隔离',
  restart_device: '重启设备',
  rebuild_device: '重建设备',
  create_ios_simulator: '创建 iOS 模拟器',
  start_ios_simulator: '启动 iOS 模拟器',
  stop_ios_simulator: '停止 iOS 模拟器',
  reimage_device: '更换 Android 镜像并重装',
  start_device: '启动设备',
  stop_device: '停止设备',
  delete_device: '删除设备',
  scale_down_device: '自动缩容设备',
  cancel_pending_device_reservation: '取消等待中的预约',
  release_device_reservation: '释放预约',
  force_release_device_reservation: '强制释放预约',
  expire_device_reservation: '预约到期回收',
  stf_release_failed: '远控服务释放失败',
  synchronize_android_system_images: '同步 Android 系统目录',
  prepare_android_system_image: '准备 Android 系统镜像',
  issue_ios_session_grant: '签发 iOS 会话授权',
  consume_ios_session_grant: '使用 iOS 会话授权',
  bind_ios_appium_session: '绑定 iOS 自动化会话',
  close_ios_appium_session: '关闭 iOS 自动化会话',
  fail_ios_appium_session: '标记 iOS 自动化会话失败',
  cleanup_ios_appium_session: '清理 iOS 自动化会话',
  quarantine_ios_session_drift: '隔离 iOS 会话漂移',
}

const resourceTypeLabels: Record<string, string> = {
  console_session: '控制台会话',
  device: '设备',
  device_image: 'Android 设备镜像',
  device_host: '宿主机',
  device_pool: '设备池',
  device_pool_image: '设备池镜像目标',
  device_reservation: '设备预约',
  android_system_image: 'Android 系统镜像',
  android_system_image_catalog: 'Android 系统目录',
}

export const deviceKindLabel = (value?: string | null) => lookup(deviceKindLabels, value)
export const providerTypeLabel = (value?: string | null) => lookup(providerTypeLabels, value)
export const lifecycleStatusLabel = (value?: string | null) => lookup(lifecycleStatusLabels, value)
export const healthStatusLabel = (value?: string | null) => lookup(healthStatusLabels, value)
export const lifecycleModeLabel = (value?: string | null) => lookup(lifecycleModeLabels, value)
export const imageStatusLabel = (value?: string | null) => lookup(imageStatusLabels, value)
export const hostTypeLabel = (value?: string | null) => lookup(hostTypeLabels, value)
export const hostStatusLabel = (value?: string | null) => lookup(hostStatusLabels, value)
export const poolStatusLabel = (value?: string | null) => lookup(poolStatusLabels, value)
export const reservationStatusLabel = (value?: string | null) => lookup(reservationStatusLabels, value)
export const ownerTypeLabel = (value?: string | null) => lookup(ownerTypeLabels, value)
export const roleLabel = (value?: string | null) => lookup(roleLabels, value)
export const actorTypeLabel = (value?: string | null) => lookup(actorTypeLabels, value)
export const severityLabel = (value?: string | null) => lookup(severityLabels, value)
export const healthSourceLabel = (value?: string | null) => lookup(healthSourceLabels, value)
export const healthEventTypeLabel = (value?: string | null) => lookup(healthEventTypeLabels, value)
export const auditActionLabel = (value?: string | null) => lookup(auditActionLabels, value)
export const resourceTypeLabel = (value?: string | null) => lookup(resourceTypeLabels, value)

export function reservationFailureLabel(value?: string | null): string {
  if (!value) return '分配未完成'
  const labels: Record<string, string> = {
    CAPACITY_UNAVAILABLE: '暂无可用设备容量',
    DEVICE_CAPACITY_UNAVAILABLE: '暂无可用设备容量',
    DEVICE_POOL_UNAVAILABLE: '设备池当前不可用',
    RESERVATION_CANCELED: '预约已取消',
    STF_CLAIM_FAILED: '远控服务占用设备失败',
    STF_CLAIM_COMPENSATED: '远控服务占用已撤销',
    SESSION_ID_GENERATION_FAILED: '设备会话创建失败',
  }
  return labels[value] ?? '预约未能完成'
}

export function healthReasonLabel(value?: string | null): string {
  if (!value) {
    return '-'
  }
  const exact: Record<string, string> = {
    'STF readiness stabilization is in progress': '正在等待远控服务状态稳定',
    'STF temporarily unavailable': '远控服务暂时不可用',
    'STF visibility recovered': '远控服务状态已恢复',
    'device is not visible through STF': '远控服务未发现该设备',
    'automatic scale down queued': '已进入自动缩容队列',
    'automatic scale down removed emulator resources': '自动缩容已清理设备运行资源',
    'emulator create command exhausted retries': '设备创建已达到最大重试次数',
    'management rebuild queued': '已进入人工重建队列',
    'health check failed': '健康检查失败',
    CREATE_RESULT_INVALID: '创建设备返回结果不完整',
    IOS_SIMULATOR_INVENTORY_MISSING: '宿主机完整清单中已找不到该模拟器，系统会保留原设备并等待恢复',
  }
  if (exact[value]) {
    return exact[value]
  }
  if (value.startsWith('DEVICE_BOOT_TIMEOUT:')) {
    const detail = value.slice('DEVICE_BOOT_TIMEOUT:'.length).trim()
    return /[\u3400-\u9fff]/.test(detail) ? `设备启动超时：${detail}` : '设备启动超时，请检查宿主机代理和设备启动日志'
  }
  if (value.startsWith('EMULATOR_DELETE_FAILED:')) {
    const detail = value.slice('EMULATOR_DELETE_FAILED:'.length).trim()
    return /[\u3400-\u9fff]/.test(detail) ? `模拟器删除失败：${detail}` : '模拟器删除失败，请检查宿主机运行资源和代理日志'
  }
  if (value.startsWith('IOS_PROVIDER_DEVICE_MISSING:')) {
    return '宿主机完整清单中已找不到该模拟器，系统会保留原设备并等待恢复，请检查 Mac 宿主机和 CoreSimulator'
  }
  return /[\u3400-\u9fff]/.test(value) ? value : `系统报告了未识别的状态原因（错误代码：${value}）`
}

export function consoleDisplayName(value: string): string {
  return value === 'Device Farm Admin' ? '设备农场管理员' : value
}
