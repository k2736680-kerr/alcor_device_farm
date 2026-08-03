# DF-006 验收证据

## 交付内容

- `internal/database`：pgx 连接、统一事务回调、Querier 接口和数据库时钟；
- `internal/repository`：Reservation、Host Command、Device 生命周期的 PostgreSQL Repository；
- Reservation `FOR UPDATE SKIP LOCKED` 领取；
- Host Command 单条 CTE 完成 `SKIP LOCKED` 选择和 leased 更新；
- Reservation 与 Command 幂等创建及载荷冲突拒绝；
- `docs/repository_concurrency.md`：事务边界、并发领取、幂等和时间规则；
- `scripts/verify-migrations.ps1 -RunRepositoryTests`：一次性真实 PostgreSQL 集成测试入口。

沿用本地 Alcor 的 `github.com/jackc/pgx/v5` 技术栈，没有引入 ORM、Redis、Kafka 或第二套迁移框架。

## 真实 PostgreSQL 验收

执行：

```powershell
$env:DEVICE_FARM_POSTGRES_BIN = "<PostgreSQL bin>"
$env:DEVICE_FARM_GO = "<go.exe>"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

结果：

```text
PASS TestReservationIdempotencyReturnsSameResource
PASS TestConcurrentReservationIdempotencyReturnsOneResource
PASS TestConcurrentReservationLockClaimsOnlyOnce
PASS TestConcurrentCommandClaimLeasesOnlyOnce
PASS TestCommandIdempotencyReturnsSameResource
PASS TestTransactionRollbackLeavesNoPartialReservation
PASS TestDatabaseClock
```

关键结果：

- 12 个并发请求复用同一 Reservation 幂等键，只产生一条数据库记录并返回同一个资源 ID；
- 两个事务同时领取唯一 pending Reservation，只有一个成功，另一个得到 `ErrNotFound`；
- 两个 Agent 同时领取唯一 pending Host Command，只有一个获得 lease；
- 相同幂等键但 owner/payload 不同会返回 `ErrIdempotencyConflict`，原记录不被覆盖；
- 事务内先创建 Reservation、再把 Device 从 ready 改为 reserved，随后故意返回错误；回滚后 Reservation 数为 0，Device 仍为 ready；
- 租约 SQL 使用 `clock_timestamp()`，并由 `database.ClockNow` 测试数据库时钟。

并发实现和测试中没有使用 `sync.Mutex` 或进程内状态保证数据库正确性。

## 工程回归

执行 `scripts/dev.ps1 -Task check`，`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。没有设置测试数据库 URL 时，Repository 集成测试明确 skip，不会误连开发或生产数据库。

## 验收结论

DF-006 的事务、行锁、`SKIP LOCKED`、幂等和数据库时钟基础已通过真实并发测试，可以进入 DF-007 Mock Provider 开发。
