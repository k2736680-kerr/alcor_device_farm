# DF-009 验收证据

## 交付内容

- `GET/POST /api/v1/device-reservations` 和 `GET /api/v1/device-reservations/{id}` 已接入现有统一鉴权、错误响应和 PostgreSQL；
- Reservation Service 校验 owner、设备池状态、设备池最大租期、能力条件和幂等键；
- Scheduler 使用数据库 FIFO pending 领取、`FOR UPDATE SKIP LOCKED` 设备锁和设备池行锁；
- 能力匹配同时检查 Pool 成员启用、Pool active、Host online 且非 draining、Device ready + healthy 和 JSONB capabilities；
- 分配事务一次性完成 Device `ready → reserved → busy`、Reservation `pending → active` 和 active Session 创建；
- Session 保存 serial、ADB Endpoint 和 Appium Endpoint，供后续 DaFit Harness 使用；
- Server 配置数据库后自动装配 Reservation Service，并启动可随服务上下文退出的 Scheduler；
- Image、Host、Pool、Device 的现有管理实现保持复用，没有新建重复资源模型，也没有写入 Alcor Run/Result 等业务表。

## 并发和一致性验收

在一次性 PostgreSQL 17.10 中执行 `TestOneHundredConcurrentReservationsUseTwoDevicesWithoutDoubleAllocation`：

- 100 个并发请求全部持久化；
- 2 条 Reservation 为 active，98 条保持 pending；
- 2 条 active Reservation 分配到 2 台不同 Device；
- 2 台 Device 为 busy；
- 创建 2 条 active Session，Session 与 Reservation 的 Device 完全一致；
- 数据库 active-device 唯一索引和事务锁共同保证没有双占、重复 active 或半成功状态。

`TestConcurrentIdempotencyCreatesOneReservation` 使用 20 个并发相同请求和相同幂等键，最终只有 1 条 Reservation，所有调用返回同一资源 ID。

`TestCapabilityMismatchRemainsPendingWithoutBlockingMatchedRequest` 先请求 API Level 35、再请求设备可满足的 API Level 34。Scheduler 跳过暂不可满足的旧预约，正确激活后一个预约；旧预约保持 pending 且没有能力错配。

`TestConcurrentSchedulersRespectPoolMaximumBelowDeviceCount` 把两台可用设备所在 Pool 的 `max_concurrency` 设为 1，再并发运行两个 Scheduler。最终只有 1 条 active Reservation 和 1 台 busy Device，证明设备池锁和锁后计数不会因并发快照而超配。

## API 验收

真实 HTTP + PostgreSQL 测试覆盖：

- 创建 pending Reservation；
- 相同幂等键和相同内容返回首次 Reservation；
- 相同幂等键但内容不同返回 `409 CONFLICT`；
- 按 ID 查询和按 owner 查询列表；
- 缺少幂等键、非法 owner 过滤返回 400；
- disabled Pool 返回 409；
- 租期超过 Pool 最大值返回 400；
- Reservation 三个北向接口无 Token 返回 401，Agent Token 返回 403。

## 验收命令与结果

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键结果：

```text
up-down-up migration: passed
PASS internal/repository
PASS TestOneHundredConcurrentReservationsUseTwoDevicesWithoutDoubleAllocation
PASS TestConcurrentIdempotencyCreatesOneReservation
PASS TestCapabilityMismatchRemainsPendingWithoutBlockingMatchedRequest
PASS TestConcurrentSchedulersRespectPoolMaximumBelowDeviceCount
PASS TestReservationAPIStoresListsAndReplaysPendingRequest
PASS TestReservationAPIRejectsDisabledPoolAndExcessLease
PASS internal/api
```

Repository、Scheduler 和 API 包按顺序使用同一个临时测试库，各包开始时自行清理数据；测试结束后 PostgreSQL 已停止，临时数据目录已删除。

## 验收结论

DF-009 已满足 Reservation API、FIFO/并发安全调度、能力匹配、幂等和 Session 原子创建的完成条件，可以进入 DF-010 续租、释放和 Reaper。
