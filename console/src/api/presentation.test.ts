import { describe, expect, it } from 'vitest'
import { apiErrorText, detailText, durationLabel } from './presentation'

describe('console presentation helpers', () => {
  it('localizes errors and keeps the trace information', () => {
    expect(apiErrorText({ message: 'remote control request timed out', code: 'REMOTE_CONTROL_TIMEOUT', requestId: 'req_1' }))
      .toBe('远控连接请求超时（错误代码：REMOTE_CONTROL_TIMEOUT；请求编号：req_1）')
    expect(apiErrorText({ message: 'unrecognized upstream failure', code: 'UPSTREAM_FAILED', requestId: 'req_2' }))
      .toBe('操作未完成，请根据错误代码联系管理员（错误代码：UPSTREAM_FAILED；请求编号：req_2）')
  })

  it('formats lease windows as readable Chinese durations', () => {
    expect(durationLabel(1800)).toBe('30 分钟')
    expect(durationLabel(90000)).toBe('1 天 1 小时')
  })

  it('renders nested event details without exposing raw JSON', () => {
    expect(detailText({ platform: 'ios', runtime_profile: { data_disk_mb: 4096 } }))
      .toBe('平台：iOS；运行规格：设备数据盘（MB）：4096')
  })
})
