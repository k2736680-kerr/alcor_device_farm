# DF-042 复用盘点

核对日期：2026-08-17。

## Alcor

`D:/AutoTestTools/Projects/Alcor` 已有 Android Worker 的 Reserve/WaitActive/Extend/Release、RunAttempt Device Snapshot，以及只允许固定设备 API 路径的服务端 Gateway。DF-042 复用其服务端持证、关联 Header 和固定白名单模式作为未来调用约束；当前没有 iOS Session Grant、Fence 或 XCUITest Executor 可直接复用。本仓库不修改 Alcor。

## DaFit

`D:/AutoTestTools/Projects/dafit_auto_platform/core/driver/appium_session.py` 已实现 Android 业务 WebDriver Session、能力、页面动作和测试生命周期。它不验证 PostgreSQL Reservation，也没有 iOS/XCUITest/Device Farm Fence。DF-042 不复制该文件；Fence 只检查路由能力并透明转发上游 WebDriver 协议。

## 本仓库与上游

- 复用现有 PostgreSQL Scheduler、Pool、Reservation、Lease、Device Session、Reaper、Reconciler、Host Command、Agent 协议和审计；
- 复用 DF-041 固定版本的 Appium Device Farm inventory/health 和本机 loopback Node；
- 复用 Appium 3、Device Farm 12.0.1、XCUITest 和 WDA 实际创建 Session；
- 以 Appium Device Farm 12.0.1 npm 固定包的 `gitHead=fb0e2b5c8610c4a689950cd1774c3c1f6a13237b` 源码为准：`getDeviceFiltersFromCapability` 对 `df:udids` 调用 `split(',')`；因此 Fence 固定为不含逗号的单值字符串，不使用 JSON 数组；
- 只新增复用矩阵允许的 Reservation Session Fence、Grant 技术绑定和漂移收敛，不新增业务 WebDriver、Case、Run、Result 或报告。
