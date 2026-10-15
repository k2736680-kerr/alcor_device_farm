# DF 释放链路复核（2026-10-14）

关联任务：既有预约释放、warm pool 自动缩容与 ADR-0043 修复验收，非新增业务模块。

## 状态：异常路径缺陷已修复，真实数据库全量回归通过（0 失败）

之前一次正常释放成功不代表异常路径通过；本轮把异常路径补齐后才给结论。
生产部署与宿主物理资源核验见文末「生产验证」。

## 已核实生产事实（修复前基线）

- 预约 61f16eb0-635f-4380-b89c-a793421c02e4 状态 released，绑定设备 cd7b9878-b33b-4751-b1c5-83430a2c325e。
- 该设备实际宿主 10.0.30.54，数据库状态 deleted。
- create dc5589e2-6115-4eff-b8f8-836972e643c4 与 delete 54664795-b07d-43a5-ab3d-341bfebf35fe 均 succeeded，宿主均为 54。
- 当时数据库 live devices=0，pending/active reservations=0，四池启用成员均为 0。
- 历史设备、预约与命令记录仍存在。此前脚本隐藏 SQL stderr，关于记录被剪除的解释错误。

## 发现的代码缺陷与修复

1. **非 fresh 池不健康设备释放回滚**：busy/reserved + unhealthy/degraded 设备转 ready
   被 `domain.Device.Transition` 健康门禁（`DEVICE_NOT_HEALTHY`）拒绝，整个 release/expiry
   事务回滚，预约/session 永久卡 active（STF 却已 release，状态不一致）。
   **修复**：`closeActiveLocked` 对非健康设备以 `quarantined` 为释放目标生命周期，
   `CloseActive` 增加 `releaseLifecycle` 参数原子更新；健康设备仍走 ready/recycling 原逻辑。
2. **释放依赖池仍 active**：池被暂停/禁用后 release 直接 ErrNotFound。
   **修复**：新增 `LoadPoolPolicyForRelease`，不按 active 过滤（释放不是分配门禁）；
   策略缺失时按安全默认（保留健康设备）继续关闭。
3. **宿主并发计数按 reservation.pool_id**：ADR-0043 后 pool_id 只是追溯字段，计数错误。
   **修复**：`LockPoolPolicyForHost` 活跃计数改为 `JOIN devices d ON d.id=r.device_id
   WHERE r.status='active' AND d.host_id=$1`——按设备真实宿主统计。
4. **scale-down 排 delete 即丢 membership**：delete 失败/超时后设备隔离，
   而 queueQuarantinedCleanup/queueScaleDown 都要求 pd.enabled → 设备成无人负责的孤儿，
   容器与宿主槽位永久泄漏（`slotOccupyingDevicePredicate` 仍把它算占用）。
   **修复**：`queueScaleDown` 排命令时不再取消 membership；membership 统一在
   `reconcileScaleDownDeletes` 确认 `deleted=true` 后关闭。并发超量计算扣除
   「已排 delete 未删完」的设备（pendingDeleteCount），避免第二个并发控制器
   看到 stale 占位数而对正常设备多排 delete（并发回归已覆盖）。
5. **清理查询被历史 failed/timed_out delete 永久阻挡**：一次失败 → 设备永远钉在
   quarantined 无任何路径再清理。
   **修复**：`queueQuarantinedCleanup` / `cleanupFailedCreates` 的 NOT EXISTS 只挡
   pending/leased；失败命令结算后下一轮重新排清理（host agent 幂等 key + 行锁防并发重复）。

修复未新增 API、表、状态，未引入第二套业务域。

## 回归证据（220 测试库 device_farm_warmpool_test，真实 Postgres，-p 1 -count=1）

新增测试：

- `internal/reservation/release_service_integration_test.go`
  - `TestReleaseUnhealthyBusyDeviceQuarantinesAndClosesReservation`：unhealthy busy
    设备释放成功，设备 quarantined/unhealthy，预约 released，session closed。
  - `TestReleaseContinuesWhenPoolIsDisabled`：池 disabled 时释放照常完成，健康设备回 ready。
  - `TestPoolPolicyCountsActiveReservationsByDeviceHost`：预约 pool_id 写 B、两台设备都在
    host A 时，host A 策略计数 = 2。
- `internal/warmpool/controller_scale_down_membership_test.go`
  - `TestScaleDownKeepsPoolMembershipUntilDeleteCompletes`：排 delete 后 membership 保留；
    delete 失败结算后仍保留且设备可再被清理路径找到；重试 delete 成功后才关闭 membership。
  - `TestFailedDeleteCommandDoesNotBlockRetryForever`：failed delete 不阻挡重试，
    同轮/下一轮重新排入 quarantined 清理命令。
- 既有并发回归 `TestConcurrentControllersQueueOneDeletePerExcessDevice` 继续通过
  （pendingDeleteCount 扣除防止并发过度删除）。

全量结果：warmpool / reservation / scheduler / repository / reaper / capacity 全部 PASS，0 失败。

## 生产验证（2026-10-15，10.0.80.220）

部署：`alcor-device-farm:release-fix-20261014`（`DEVICE_FARM_COMMIT=49a1474`），healthy。

E2E 全链路（预约 → 建机 → 分配 → 释放 → 缩容 → 物理删除）：

| 步骤 | 结果 |
|---|---|
| 预约 362b5992（api35，不指定池） | pending，pool_id=149c3632（池 54，追溯字段） |
| 建机 | 只建 1 台，device=40426e82，宿主 **10.0.30.54**，无重复建机 |
| 分配 | 设备 ready → 预约 active（busy），耗时约 2 分钟 |
| 释放 | API 返回 released |
| 缩容 | ready → stopped → deleted，约 60 秒（total_target=0） |
| membership | 删除确认后关闭，最终 enabled 成员 0 |
| 命令 | create:succeeded + delete:succeeded（各 1 条） |
| 物理删除 | agent 在 10.0.30.54 回报 `{"deleted":true,"provider_ref":"emulator-40426e82..."}` |
| STF | 96 台在架设备中无 40426e82（容器确实被删） |
| 终态 | live=0，open_res=0，enabled members=0，无 pending 命令 |

结论：本轮复核发现的异常路径已修复并有真实库回归；正常链路生产端到端验证通过，
数据库状态、宿主 agent 物理执行结果、STF 三者一致，资源真实释放。
