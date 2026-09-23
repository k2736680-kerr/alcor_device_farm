# DF-048 统一 Android 与 iOS 设备池自动伸缩验收证据

验收日期：2026-08-20

## 结论

通过。iOS 设备池与 Android 设备池统一使用 `total_target/min_ready/max_concurrency` 表达目标容量；Console 的 iOS 目标设备数可编辑。iOS 扩容从本池已选择的健康模板读取 Mac Host、iOS Runtime 和 iPhone Device Type，创建全新 CoreSimulator；自动创建不会再次增加目标。缩容只选择没有开放预约、活动 Session、在途 Host Command 或其他池成员关系的空闲设备。

## 自动化证据

在项目一次性 PostgreSQL 17.10 实例 `127.0.0.1:55432/device_farm_df004` 上执行完整迁移 `up -> down -> up` 和 Repository 集成测试：

- 数据库约束、旧 Android 数据回填、向下迁移和再次向上迁移通过；
- `TestConcurrentControllersScaleIOSPoolToTargetWithoutOverbuilding` 通过：当前 3、目标 6 时两个 Controller 合计只创建 3 条 iOS Device、membership 和 create command，重复收敛不超建，目标仍为 6；
- 新建命令的 `runtimeId` 和 `deviceTypeId` 与模板一致；
- `TestIOSScaleDownProtectsReservationsAndUsesIOSDeletePayload` 通过：活动预约设备和目标大于零时的模板不删除，空闲 iOS Simulator 的 delete command 带正确平台、Provider 和 UDID；
- 既有 Android 自动扩缩容、并发删除、容量不足、重建和回收测试全部通过；
- iOS Session Fence、预约、Scheduler、Reaper、Reconciler、Host Command 和管理 API 集成测试全部通过。

Console 验收：

- `PoolsPage.test.tsx` 3 项通过，其中 iOS 目标输入可编辑，提交的 `total_target/min_ready/max_concurrency` 均为用户填写值；
- 当前数量与目标数量来自列表 API；目标差额、模板缺失、Host 离线/排空、内存/磁盘/设备名额不足均有中文说明；
- `pnpm generate && tsc -b && vite build` 生产构建通过。

## 真实能力复用说明

本任务没有新增另一套 iOS Provider。实际创建和删除继续走 DF-044～DF-047 已验收的 Host Command、macOS Host Agent、Xcode CoreSimulator、Appium Device Farm、XCUITest/WDA 链路。本次只增加 Pool 目标收敛和安全选择逻辑，不复制 Simulator 数据，不接触 Alcor Run、DaFit Runner 或 STF 内部实现。
