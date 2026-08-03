# DF-012 验收证据

## 交付内容

- `POST /internal/v1/device-hosts/{id}/heartbeats`：Host 心跳、容量和发现设备摘要；
- `POST /internal/v1/device-hosts/{id}/commands/claims`：支持 1~20 条批量领取和 0~30 秒等待；
- `POST /internal/v1/device-host-commands/{id}/completions`：校验 lease token、attempt、状态和租约有效期；
- Host Command Service 复用 DF-006 的 `device_host_commands`、幂等键和 `SKIP LOCKED` Repository；
- Heartbeat 使用 Host 状态机把 offline Host 恢复为 online，`last_heartbeat_at` 使用数据库接收时间；Agent 自报时间不作为租约真相；
- Completion 只接受 `succeeded/failed/timed_out`，旧 token、旧 attempt、已过期 lease 或重复 completion 返回稳定冲突；
- 后台 lease recovery 使用 PostgreSQL 时钟：未到最大尝试次数返回 pending，达到上限进入 timed_out 并写错误码和完成时间；
- Server 自动装配 Host Command Service 和过期 lease 恢复循环；
- Agent Token 仍不能调用任何 `/api/v1` 北向管理接口。

## 并发与租约验收

真实 PostgreSQL 17.10 测试：

- `TestConcurrentClaimsLeaseOneCommandOnce`：两个 Agent 同时领取唯一命令，合计只拿到 1 条；
- `TestExpiredLeaseReclaimsAndRejectsOldCompletion`：第 1 次 lease 过期后安全返回 pending，第 2 次领取得到新 token 和 attempt=2；旧 completion 被拒绝，新 completion 成功；
- `TestExpiredFinalAttemptTimesOut`：最大尝试次数为 1 的命令过期后进入 timed_out，不再被领取；
- Repository 原有命令创建幂等和并发 claim 测试继续通过。

## Heartbeat 与 API 验收

- `TestHeartbeatBringsOfflineHostOnline`：offline Host 收到有效心跳后通过状态机变为 online，保存数据库接收时间和容量；
- 重复 provider_ref/serial、非法租约范围、非法 completion 状态在 Service 层拒绝；
- `TestAgentHeartbeatClaimAndCompletionAPI` 通过真实 HTTP 完成 heartbeat → claim → completion；
- internal API 无 Token 返回 401，Service Token 返回 403，Agent Token 正常调用；
- 已完成命令再次 completion 返回 `409 STALE_COMMAND_LEASE`。

## 复用与边界

- 没有新增消息队列或第二张命令表；
- 命令状态仍调用 DF-005 Command 状态机；
- lease 领取继续使用 DF-006 单条 CTE；
- API 路径和字段与既有 OpenAPI 完全一致；
- 本阶段只提供协议和 Server 端能力，Agent 进程的领取/执行循环在 DF-013 实现；
- 没有复制 DaFit 执行器，也没有创建 Alcor 业务表。

## 验收命令与结果

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键结果：

```text
PASS TestHeartbeatBringsOfflineHostOnline
PASS TestConcurrentClaimsLeaseOneCommandOnce
PASS TestExpiredLeaseReclaimsAndRejectsOldCompletion
PASS TestExpiredFinalAttemptTimesOut
PASS TestAgentHeartbeatClaimAndCompletionAPI
PASS internal/hostcommand
PASS internal/api
```

`scripts/dev.ps1 -Task check` 完成格式、静态检查、全量测试和 Server/Agent 构建。临时 PostgreSQL 已停止并清理。

## 验收结论

DF-012 已满足 Heartbeat、命令领取/完成、lease、幂等、超时和重试上限的完成条件，可以进入 DF-013 Host Agent 核心程序。
