# DF-010 验收证据

## 交付内容

- `POST /api/v1/device-reservations/{id}/extensions` 已实现合法续租；
- `POST /api/v1/device-reservations/{id}/releases` 支持主动释放和 `force=true` 强制释放；
- extension/release 复用 `device_idempotency_records`，相同请求不会重复延长或重复清理；
- 释放事务调用已有 Reservation、Session 和 Device 状态机，并一次性完成：
  - Reservation `active → released/force_released/expired`；
  - Session `active → closing → closed`；
  - Device `busy → recycling`；
  - 写入 `device_audit_events`；
- Reaper 使用 PostgreSQL `clock_timestamp()`、grace period 和 `FOR UPDATE SKIP LOCKED` 回收过期预约；
- Server 自动启动 Reaper，并随服务上下文退出；
- Scheduler 间隔、Reaper 间隔和 grace period 已加入 YAML/环境变量配置，默认分别为 `250ms`、`1s`、`30s`。

没有增加新的租约表、Session 表或设备状态模型；全部复用 DF-004 至 DF-009 已有结构。STF 底层 release 留在 DF-017 Adapter 接入，当前不会伪造 STF 已释放。

## 续租验收

`TestExtensionUsesDatabaseLeaseAndIsIdempotent` 在真实 PostgreSQL 中验证：

- active Reservation 合法增加 300 秒；
- 同一幂等键重放不会再次增加 300 秒；
- 同一幂等键更换参数返回冲突；
- 总租期超过 Pool `max_lease_seconds` 被拒绝；
- 已经过期但尚未被 Reaper 处理的 Reservation 不允许续租复活；
- 到期判断使用数据库时钟，而不是 Server 进程时间。

## 释放与审计验收

`TestConcurrentReleaseClosesOnlyOnce` 同时发起两次 release，两个调用均幂等成功，最终只有：

- 1 个 released Reservation；
- 1 个 closed Session；
- 1 台 recycling Device；
- 1 条释放审计事件。

`TestForceReleaseWritesReasonedAudit` 验证强制释放进入 `force_released`，并保存明确 action、actor、request ID 和 reason。缺少或过短 reason 在 API/Service 校验阶段被拒绝。

## Reaper 并发与 grace period 验收

`TestTwoReapersCloseExpiredReservationOnce` 让两个 Reaper 实例同时扫描同一条超过 grace period 的预约：一个成功、一个得到无可回收任务，最终只生成一次终态和一次审计记录。

`TestReaperHonorsGracePeriod` 把预约设置为刚过期 10 秒，配置 30 秒 grace period；Reaper 不提前回收，Reservation 保持 active。

## HTTP 与契约验收

`TestReservationAPIExtendsAndReleasesActiveReservation` 通过真实 HTTP、鉴权、PostgreSQL 和 Scheduler 完成：创建 → active → extension → release → 幂等重放。

OpenAPI 已增加独立 `ReservationRelease` Schema，明确 `reason` 必填、`force` 可选；extension/release 无 Token 返回 401，Agent Token 返回 403。

## 验收命令与结果

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键结果：

```text
PASS TestExtensionUsesDatabaseLeaseAndIsIdempotent
PASS TestConcurrentReleaseClosesOnlyOnce
PASS TestForceReleaseWritesReasonedAudit
PASS TestTwoReapersCloseExpiredReservationOnce
PASS TestReaperHonorsGracePeriod
PASS TestReservationAPIExtendsAndReleasesActiveReservation
PASS internal/repository
PASS internal/scheduler
PASS internal/reaper
PASS internal/api
```

测试结束后 PostgreSQL 已停止，临时数据目录已删除。`scripts/dev.ps1 -Task check` 负责全量格式、静态检查、单元测试和构建验证。

## 验收结论

DF-010 已满足续租、主动/强制释放、grace period、并发幂等和多 Reaper 安全回收的完成条件，可以进入 DF-011 Reconciler、健康事件和隔离。
