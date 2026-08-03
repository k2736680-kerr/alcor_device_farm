# DF-008 验收证据

## 交付内容

- 23 个 Image、Host、Pool、Device 管理操作已按 `openapi/device-farm-v1.yaml` 注册；
- `internal/api`：严格 JSON 解码、请求大小限制、幂等 Header 校验、统一错误映射和响应；
- `internal/management`：资源 Service、领域状态机调用、仅用于 E0 测试准备的 Mock Device 和真实设备 Host Command 编排；
- `internal/management/postgres`：四类资源 Store、通用创建幂等、Pool 成员、可调度查询，以及 Device 状态、审计和 Host Command 的原子写入；
- Server 在配置 `DEVICE_FARM_DATABASE_URL` 后自动连接 PostgreSQL；Mock Provider 只用于 E0 测试准备，不再由 restart/rebuild 管理 API 直接调用；未配置数据库时管理接口返回 503；
- `000002_api_idempotency` migration：只保存设备 API 请求哈希和设备资源 ID，不保存 Token、请求正文或 Alcor 业务对象。

## 真实 API 全链路

在一次性 PostgreSQL 17.10 和 Mock Provider 上完成：

1. 创建、重放、查询、更新 Image，并发起 validation；
2. 创建、查询、更新 Host；验证 offline Host 不能 drain，online Host 可 drain/undrain；
3. 创建、查询、更新 Pool，添加/移除 Device；
4. 通过已验证 Image 和 online Host 准备 Mock Device；
5. 查询 Device；restart/rebuild 原子创建持久化 Host Command，由 Agent 领取并完成，成功健康快照恢复 ready，最终失败自动隔离；同时执行 quarantine/unquarantine；
6. quarantined Device 从可调度查询中消失；
7. quarantined Device restart 返回 409；
8. disabled Pool 拒绝添加 Device；
9. draining Host 拒绝准备第二台 Device。

相同 Image 创建请求与相同幂等键返回首次资源 ID；同 key 换请求内容返回 `409 CONFLICT`，没有覆盖原资源。

restart/rebuild 使用请求幂等键生成 Host Command 作用域键；重放请求不重复命令、状态转换或审计，同 key 换设备、动作或请求内容返回冲突。

## 权限、参数和状态验收

`TestEveryManagementRouteIsProtected` 对全部 23 个操作逐一验证：

- 无 Token：401；
- Agent Token 调北向管理 API：403。

`TestManagementAPIRejectsInvalidParameters` 覆盖 Image/Host/Pool 空参数、缺失幂等键、Pool Device 缺失 ID、四类 Device action 缺失 reason、非法 JSON 和未知字段，均在进入 Store/Provider 前返回 400。

非法状态覆盖：

- validating Image 再次 validation；
- offline Host drain；
- quarantined Device restart；
- disabled Pool 添加成员。

以上均返回 409，数据库状态不被绕过修改。

## 验收命令与结果

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键结果：

```text
up-down-up migration: passed
PASS internal/repository
PASS TestManagementAPICompleteMockFlow
PASS TestEveryManagementRouteIsProtected
PASS TestManagementAPIRejectsInvalidParameters
```

Repository 与 API 真实数据库测试按包串行执行，避免共享临时库互相清理。测试结束后 PostgreSQL 已停止，临时数据目录已清理。

执行 `scripts/dev.ps1 -Task check`，`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。

## 验收结论

DF-008 的核心管理资源已经能够在现有独立设备农场架构上真实运行，并保持与 Alcor 的 API/数据边界；可以进入 DF-009 Reservation API 和 Scheduler 开发。
