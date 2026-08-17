# DF-040 平台中立设备域模型与契约验收

## 当前状态

`in_progress`

DF-040 已完成实现和本地验收，等待独立 Git 提交及清单回填后标记 `completed`。本任务只建立 Android/iOS 共用的设备域数据和调度契约，没有连接真实 Appium Device Farm、macOS Agent 或 iOS Session Fence。

## 实现结果

- 新增 `000014_platform_neutral_device_domain` up/down migration：Host 增加 `host_os/host_arch`，Pool 和 Device 增加 `platform`，旧数据回填 `linux/unknown/android`；
- Device 支持 Android `emulator/physical` 与 iOS `simulator/physical`，Provider 增加 `appium_device_farm_ios`；
- 数据库 Trigger 拒绝 Android/iOS 混合 Pool membership，平台变更也不能绕过该约束；
- iOS Device 可共享 Appium Node Endpoint；Android 活动 Endpoint 仍唯一，serial/UDID、Provider identity 和 active Reservation 唯一性没有放宽；
- OpenAPI 冻结版本提升为 `2.0.0`，Host、Pool、Device 的平台字段和枚举成为明确契约；
- Provider 增加 `transport/os_ready/automation/router/remote_control` 组件健康模型，`unsupported` 的 iOS 人工远控不被误判为失败；
- Scheduler 先匹配 `Pool.platform=Device.platform`，规范化 `platformName`，iOS Mock 预约不会误调用 Android STF claim；
- 只有登记的标准能力参与匹配；未知字段作为非权威预约扩展保留，不会意外卡住选机；
- Android Warm Pool 只处理 `platform=android`，DF-040 不提前建设 iOS Simulator 生命周期。

## 验收证据

2026-08-17 在 Windows 11、Go 工具链和一次性 PostgreSQL 17.10 实例执行：

1. `scripts/verify-migrations.ps1 -RunRepositoryTests`
   - migration `up → down → up` 通过；
   - 在 `000013` schema 插入旧 Host/Pool/Device 后执行 `000014`，`legacy Android backfill: passed`；
   - Repository 验证 iOS 共享 Endpoint 合法、跨平台入池拒绝、重复活动 UDID 拒绝；
   - Scheduler 的 100 个并发 iOS 请求竞争一台 Mock iOS Device，结果为 1 个 active Reservation、1 个 active Session、1 台 busy Device；
   - Repository、Scheduler、Reaper、Reconcile、Host Command、Metrics、API、Warm Pool 串行 PostgreSQL 集成回归全部通过。
2. `go test ./...` 通过，包含 Android 第一版全部本地 Go 回归；数据库集成用例由上一项显式提供测试库，未以 skip 结果代替。
3. OpenAPI hash、migration contract、平台中立组件健康与 iOS Mock 共享 Endpoint 单元测试通过。

## 边界

- 未接入真实 macOS、Xcode、iPhone、Simulator、WDA 或 Appium Device Farm；这些属于 DF-041～DF-046；
- 本次 Mock 和 PostgreSQL 结果只证明 DF-040 控制面契约，不冒充真实 iOS Session 验收；
- 未新增 App、Build、Case、Run、Result、Artifact，也未复制 DaFit Runner、STF 或 Appium WebDriver 业务执行能力。
