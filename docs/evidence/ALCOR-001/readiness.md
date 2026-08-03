# ALCOR-001 接入准备审计

审计时间：2026-08-03

审计仓库：`E:/AutoTestTools/Projects/Alcor`

审计分支：`origin/feature/refactoring`

审计提交：`868de61d7534639114e3feb0369d99c8e7f9c91a`

## 结论

新版 Alcor 已出现正式 Run、RunAttempt、PostgreSQL 租约 Worker、ClickHouse 结果和 Supabase Artifact 实现，说明平台主干方向已经与设备农场方案一致；但 ALCOR-001 仍未满足安全开工条件，当前保持 `waiting_external`。

## 已满足

- `platform_runs` 和 `platform_run_attempts` 使用 UUID；
- Worker 通过 PostgreSQL `FOR UPDATE SKIP LOCKED` 领取 queued Attempt；
- Attempt 具有 `worker_id`、`lease_token`、`lease_expires_at` 和 running/failed/canceled 等状态；
- Worker 已写运行事件、ClickHouse 用例结果和 Supabase Artifact；
- 基础设施失败事件已使用 `run.infra_failed`。

## 尚未满足

| 检查项 | 当前事实 | ALCOR-001 要求 |
|---|---|---|
| 正式 OpenAPI | `alcor_server/docs/swagger.yaml` 仍只发布旧 `/eval-tasks`、`/datasets`、`/test-items`，没有实际 `/runs` 和 RunAttempt | 发布与真实 `/api/v1`、`/api/v2` 路由一致的 Run/RunAttempt OpenAPI |
| Device Farm Adapter | Worker 只有 HTTP/WebSocket 等协议执行逻辑，没有 Device Farm 调用边界 | Worker 明确注入 Device Farm Adapter，并按 RunAttempt 申请/续租/释放 |
| 配置 | `pkg/config` 和 secrets 示例没有 Device Farm Endpoint、Token 引用、Pool、超时和重试预算 | 使用密钥引用配置，不把 Token 写入数据库或普通日志 |
| Android 输入模型 | 当前 Snapshot 只解析 Target/Endpoint BaseURL，没有 Android App、设备能力或执行器选择 | 新版 Alcor 明确 Android Case/Run 输入，或提供经确认的专项契约 |
| 关联 Header | 当前 Worker 写 `X-Alcor-Run-Id`、`X-Alcor-Attempt-Id` | 已确定方案要求 `X-Eval-Run-Id`、`X-Eval-Attempt-Id` 和 `traceparent` |
| 取消和释放 | Worker 可判断 Run canceled，但没有设备预约可释放 | 成功、失败、取消、超时、进程退出均幂等 release |

## 可重复检查

```powershell
.\scripts\check-alcor-integration-readiness.ps1 `
  -AlcorRoot 'E:\AutoTestTools\Projects\Alcor' `
  -Ref 'origin/feature/refactoring'
```

只有输出 `ALCOR_DEVICE_FARM_READY=true` 后才进入真实 Alcor 代码接入。当前不能根据内部 SQL 和未发布路由猜测最终 OpenAPI，也不能把本仓库 Mock 当成生产联通。
