# DF-042 Reservation 绑定的 iOS Session Fence 验收

## 当前状态

`completed`

执行裁决见 ADR-0022：复用既有 `device_sessions`、Reservation/Reaper、Host Agent、Agent Token 和 Appium Device Farm Adapter；不复制 DaFit WebDriver Session，也不在本仓库实现 Alcor iOS Executor。

## 验收环境

- E4 macOS arm64 Host，Host ID、地址和账户已脱敏；
- allowlist 中一台 booted iOS Simulator，UDID 已脱敏；
- Node 22.23.2、Appium 3.6.0、Appium Device Farm 12.0.1、XCUITest 12.4.0、WDA 16.2.0、go-ios 1.3.2；
- Appium Node 仅监听 Host loopback，Session Fence 通过受控 SSH 端口转发供本次 Server 验收；
- PostgreSQL 17 临时验收库；所有 Token、SSH 密码和 Session Grant 均未写入仓库或本文件。

## 自动化与真实链路结果

| 场景 | 结果 |
|---|---|
| Grant 签发、消费、过期、重放、跨 Host、错误/多 UDID、唯一约束 | 真实 PostgreSQL 集成测试通过 |
| Release、Reaper、清理失败隔离、无 Reservation busy 漂移 | 真实 PostgreSQL 集成测试通过 |
| Fence Dashboard/插件 API 拒绝、Agent 清理鉴权、绑定后精确 Session 路由 | Go 测试通过 |
| `df:udids` 格式 | 固定包源码确认 12.0.1 使用逗号分隔字符串；数组会触发插件异常，因此 Fence 只接受与 `appium:udid` 相同且不含逗号的单值字符串 |
| 真实 XCUITest Session | `POST /session` 返回 200，Session ID 仅记录为 `***2a1230` |
| Grant 重放 | 同一 Grant 第二次 `POST /session` 返回 409，未创建第二 Session |
| 绑定后请求 | `GET /session/{id}` 返回 200；release 后同一路径返回 409 |
| 活动 Session 健康语义 | Host `online`；Device `busy/healthy`；`providerBusy=true`，未被心跳误隔离 |
| Release 收敛 | Reservation `released`；Device Session `closed`；Appium Session 有结束时间；Device `ready/healthy`；`providerBusy=false` |
| WDA 常驻端口与 doctor | WDA 端口存活时，doctor 可选 Remote XPC 探测被 15 秒上限截断；超过 30 秒缓存周期后 Host 仍 `online`、doctor 必需检查为 passed |
| 清理心跳竞态 | 最近正常结束 Session 提供 30 秒 busy 刷新宽限；超时仍 busy 的集成测试继续隔离 |
| 明文 Grant | `device_sessions` 只有 64 字符 SHA-256 与时间字段；Windows/Mac 验收日志敏感模式扫描为 0 |
| STF 边界 | iOS Session 创建、路由和清理未调用 STF |

## 数据库终态证据

最终真实 Reservation 为 `released`，对应 Device Session 为 `closed`；`session_grant_hash` 长度为 64，Grant 已消费，Appium Session 已绑定且已结束。数据库列只有 `session_grant_hash`、`session_grant_expires_at`、`session_grant_consumed_at`，没有明文 Grant 列。

## 门禁

- `git diff --check`：通过；
- `go vet ./...`：通过；
- `go test ./...`：通过；
- PostgreSQL 17 migration `up → down → up` 和真实 Repository/Scheduler/Reaper/Reconcile/Host Command/Metrics/API/Warm Pool/iOS Session 门禁：通过；
- Android 相关 Go 回归包含在全量测试中，未引入 STF/Appium 业务执行复制。

## 结论

DF-042 满足短时单次 Grant、Reservation/Host/UDID 固定路由、单设备单活动 Appium Session、受控清理、漂移隔离和 Secret 不落库边界，可以进入 DF-043。DF-043 仍需按任务要求完成两台 Simulator 并发和 50 次稳定性验收，本任务的一台真实 Simulator 只证明 Session Fence 闭环。
