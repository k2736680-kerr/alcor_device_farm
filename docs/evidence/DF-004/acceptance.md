# DF-004 验收证据

## 交付内容

- `migrations/000001_device_domain.up.sql`：11 张设备域表、外键、检查约束、查询索引和唯一索引；
- `migrations/000001_device_domain.down.sql`：按依赖逆序删除设备域表；
- `migrations/test/constraints.sql`：真实数据库唯一约束与业务域隔离测试；
- `scripts/verify-migrations.ps1`：创建一次性 PostgreSQL、执行 `up → down → up` 并安全清理；
- `docs/database_model.md`：数据边界、ER 图、核心约束和回滚说明；
- `internal/contract/migration_test.go`：迁移文件配对、11 张表、关键索引和禁建业务表的静态契约测试。

## 测试环境

- PostgreSQL：17.10 Windows x64 免安装二进制；
- 来源：PostgreSQL 官方 Windows 下载页指向的 EDB 二进制归档；
- ZIP SHA-256：`EF9B1E5E23D2E8A83914BA13D9DC536A72210FBA53FD1808FF1F7E06BB22B106`；
- 测试实例：只监听 `127.0.0.1:55432`，位于项目忽略的 `tmp` 目录，不注册系统服务，验收结束后已停止并清理。

## 真实数据库验收

执行：

```powershell
$env:DEVICE_FARM_POSTGRES_BIN = "<PostgreSQL bin>"
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1
```

结果：

```text
constraint checks: passed
down migration: passed
up-down-up migration: passed
```

数据库实际拒绝了：

- 第二条相同 `devices.serial`；
- 同一 `client_id + idempotency_key` 的第二条预约；
- 同一 Device 的第二条 `active` 预约。

down 后确认没有遗留 `device_%` 表；第二次 up 后确认恰好恢复 11 张设备域表。数据库中同时确认不存在 `eval_tasks`、`eval_results`、`runs`、`run_attempts`、`run_results` 和 `artifacts`。

## 工程回归

执行 `scripts/dev.ps1 -Task check`，`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。

## 验收结论

DF-004 的 migration 可逆，关键并发与幂等不变量由 PostgreSQL 自身保证，且没有混入 Alcor 业务域表，可以进入 DF-005 领域模型和状态机开发。
