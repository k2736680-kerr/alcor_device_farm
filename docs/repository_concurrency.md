# Repository、事务与并发规则

## 技术选择

设备农场沿用现有 Alcor 使用的 `pgx/v5` PostgreSQL 驱动，不引入 ORM、Redis 或进程内锁。`internal/database` 只提供连接、事务和数据库时钟；`internal/repository` 保存设备域 SQL。

## 事务边界

需要同时改变多个聚合的 Service 必须使用 `database.DB.WithinTx`，并把同一个 `pgx.Tx` 传给所有 Repository。回调返回任何错误都会回滚，只有回调成功才提交。

典型预约分配事务：

```text
BEGIN
  锁定一条 pending reservation（FOR UPDATE SKIP LOCKED）
  锁定一台 ready + healthy device（DF-009）
  领域对象校验 pending→active、ready→reserved
  更新 reservation 和 device
COMMIT
```

STF、Appium 和 Provider 网络调用不得放进数据库事务；它们在提交后执行，并通过补偿事务恢复或隔离设备。

## 并发领取

- Reservation 和 Host Command 都按 `created_at, id` 排序；
- 使用 `FOR UPDATE SKIP LOCKED`，其他 Worker 不等待已被领取的行；
- Command 在同一条 CTE 中完成“选取 + leased 更新”，不能先 SELECT 再脱离事务 UPDATE；
- 数据库唯一索引仍是最终防线，Go 代码不使用 mutex 维护正确性。

## 幂等规则

- Reservation 使用 `(client_id, idempotency_key)` 唯一约束；
- Host Command 使用 `(host_id, idempotency_key)` 唯一约束；
- 相同 key 且请求字段一致时返回首次创建的资源 ID；
- 相同 key 但 owner、pool、能力、租期、命令类型或 payload 不一致时返回 `ErrIdempotencyConflict`，不覆盖原记录。

## 时间规则

租约创建、更新时间和到期判断使用 SQL `clock_timestamp()` 或 `database.ClockNow`。应用进程的 `time.Now()` 只用于本地超时控制和测试窗口，不能决定数据库租约所有权。
