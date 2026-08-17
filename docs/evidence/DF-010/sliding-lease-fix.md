# DF-010 长任务滑动续约修正证据

日期：2026-08-17

## 问题与结论

原实现把 Pool `max_lease_seconds=3600` 当作从 Reservation `starts_at` 起计算的总寿命上限。Alcor Worker 虽然在 DaFit 运行期间每 10 分钟调用一次 extension，但累计运行到一小时后仍会被拒绝续约，随后可能被 Reaper 回收。

本次按 ADR-0023 改为有限滑动窗口：

- active 且未过期的预约可在真实任务存活期间持续续约，累计运行超过四小时不再锁死；
- `expires_at` 不能晚于数据库当前时间加 Pool `max_lease_seconds`，失联占用仍然有界；
- 单次 `additional_seconds` 不能超过 Pool 最大窗口；
- 已过期预约不能续约复活；
- DaFit Harness 默认不设置固定运行时长上限，在 Runner 存活期间按租期三分之一周期续约，运行结束立即停止续约并 release；调用方仍可显式设置业务超时；
- 续约失败会取消 Runner 并进入 release，不会静默继续使用已失效设备；
- Console 远控心跳同步取消从首次开始时间计算的总寿命限制；
- 相关 API、Adapter 和 Harness 用户提示已改为中文，稳定错误码保持不变。

没有新增表、状态或 Alcor 业务对象，也没有复制 DaFit Runner、Appium WebDriver、页面对象、断言、证据或报告能力。

## 真实 PostgreSQL 验收

使用 PostgreSQL 17.10 一次性实例执行：

```powershell
scripts/verify-migrations.ps1 -PostgresBin E:\AutoTestTools\Tools\PostgreSQL-17.10\pgsql\bin -Port 55433 -RunRepositoryTests
```

结果：

- migration `up → down → up`：通过；
- 旧 Android 数据回填和数据库约束：通过；
- iOS Session Fence、Repository、Scheduler、Reaper、Reconciler、Host Command、Metrics、API、Warm Pool 集成门禁：全部通过；
- `TestReservationAPISlidesLeaseBeyondFourHoursWithoutExceedingFutureWindow`：通过，数据库中的 `starts_at` 已超过五小时，连续续约仍保持 active，未来窗口未超过 3600 秒；
- 超过 Pool 单次最大窗口：返回 400 和明确中文提示；
- 已过期预约续约：返回 409 和明确中文提示；
- PostgreSQL 测试实例已停止，临时数据目录已清理。

## Harness 与全量回归

- `TestHarnessKeepsReservationAliveUntilDaFitFinishes`：运行期间发生多次续约，结束后只 release 一次；
- `TestHarnessStopsDaFitAndReleasesWhenLeaseRenewalFails`：续约失败会停止执行并 release；
- `go test ./...`：通过；
- Console Vitest：7 个测试文件、32 项测试全部通过；
- Console OpenAPI 生成、TypeScript 和生产构建：通过；构建仅保留已有的大 chunk 警告。

## 安全边界

总运行时间不设固定上限不等于永久预约。只要 Worker/Harness 停止发送 extension，现有 Reaper 仍会在 `max_lease_seconds + grace period` 的有限时间内回收设备。正常成功、失败、取消或超时路径仍立即 release。
