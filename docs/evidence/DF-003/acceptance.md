# DF-003 验收证据

## 交付内容

- `openapi/device-farm-v1.yaml`：25 个路径、35 个操作，覆盖 System、Image、Host、Pool、Device、Reservation 和 Agent MVP 接口；
- `internal/auth`：服务 Token 与 Agent Token 分域认证，Token 缺失或错误返回 401，身份域错误返回 403；
- `internal/contract`：契约结构、全部路径/方法、唯一 `operationId`、安全方案、本地 `$ref`、预约示例和历史接口隔离检查；
- 配置校验拒绝服务 Token 与 Agent Token 使用相同值；未配置 Token 时受保护接口默认关闭；
- `/healthz`、`/readyz` 保持公开，其他 `/api/v1/` 和 `/internal/v1/` 路径在进入业务路由前统一认证。

## 复用与边界核对

- 已搜索本地 Alcor：现有 master 只有历史评估任务接口和通用 Bearer 调用代码，没有设备域 OpenAPI、Host Agent 协议或可直接复用的双身份中间件；
- 已搜索 DaFit：保留现有 Appium/ADB/Runner 能力，本步骤没有复制执行器；
- 契约不包含 Alcor Case、Dataset、Run、RunAttempt、Result、Artifact，也不依赖历史评估任务接口；
- 北向 Owner 继续使用 UUID/ULID 字符串，预约示例使用 `owner_type=test_run`，未来可原样使用 `run_attempt`。

## 验收命令与结果

执行：

```powershell
go test -v ./internal/contract ./internal/auth ./internal/server ./internal/config
```

结果：

```text
PASS internal/contract: TestOpenAPIContract
PASS internal/auth: 401、403、服务身份、Agent 身份、公开路径和空配置关闭测试
PASS internal/server: 统一响应、关联 ID、404/405 测试
PASS internal/config: 双 Token 冲突和敏感字段不序列化测试
```

执行完整工程检查：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/dev.ps1 -Task check
```

结果：`gofmt`、`go vet ./...`、`go test ./...`、Server 构建和 Agent 构建全部通过。

## 验收结论

DF-003 的 OpenAPI、双身份认证和契约测试均通过。接口契约已经能作为后续 DF-008、DF-012 以及新版 Alcor Device Farm Adapter 的稳定边界。
