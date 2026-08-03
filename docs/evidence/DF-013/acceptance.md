# DF-013 验收证据

## 交付内容

- `device-host-agent` 已从占位程序变为可独立运行的 Host Agent；
- Agent 使用预先在设备农场管理面创建的 Host ID 绑定宿主机，不新增自注册接口，也不让 Agent Token 越权调用北向 Host 管理 API；
- HTTP Client 复用 DF-012 已冻结的 heartbeat、command claim、command completion 协议，并携带独立 Agent Bearer Token；
- Agent 周期调用 Provider `Discover`，把本机设备、容量、ADB/Appium 连接摘要上报 Server；
- Agent 使用长轮询领取 PostgreSQL lease 保护的命令，支持 create/start/stop/restart/rebuild/delete/inspect；
- 命令完成回报携带原 lease token 和 attempt，Provider 错误统一映射为稳定错误码；
- 使用固定并发槽限制本机 Provider 操作，领取数量不会超过剩余槽位；
- 收到 SIGINT/SIGTERM 后立即停止领取新命令，并等待在途命令完成；超过退出等待时间时明确报错，由 DF-012 Server lease recovery 恢复未完成命令；
- Agent Server URL、Host ID 和 Token 均通过参数或环境变量注入，仓库未保存真实 Token；
- 当前运行入口使用 DF-007 Mock Provider；真实 Docker/KVM Provider 在 DF-014 接入，不以 Mock 冒充真实设备验收。

## 核心运行链路验收

`TestAgentStopsClaimingAndFinishesInflightCommands` 确定性验证：

- 队列预置 3 条 restart 命令，并发上限为 2；
- 同时最多执行 2 个 Provider 操作；
- 退出信号到达后不再领取第 3 条命令；
- 两条在途命令均完成 completion；
- Agent 运行期间至少完成一次设备发现和 heartbeat；
- 修复取消信号与领取循环同时就绪时可能额外领取一批命令的退出竞态。

Agent 或 Server 重启时，命令真相仍保存在 DF-012 的 PostgreSQL `device_host_commands` 中：未完成 lease 到期后按 attempt 和最大重试次数安全重领，旧 lease completion 不能覆盖新 attempt。该能力已经由 DF-012 真实 PostgreSQL 验收覆盖，本阶段没有新建进程内命令队列。

Host 心跳超时后的 offline 收敛继续复用 DF-011 Reconciler，不在 Agent 中重复实现 Host 状态判断。

## HTTP 协议验收

- `TestHTTPClientClaimUsesAgentTokenAndDecodesEnvelope`：验证内部 API 路径、POST 方法、Bearer Token、JSON 请求、统一 `data/error` 响应包和命令列表解码；
- `TestHTTPClientReturnsAPIErrorForNonSuccessStatus`：验证非 2xx 响应保留 Server 错误码和错误信息；
- HTTP Client 只调用 `/internal/v1`，没有调用 `/api/v1` 北向管理接口。

## 复用与边界

- 复用 DF-007 Provider 接口和 Mock Provider，没有在 Agent 内复制设备生命周期实现；
- 复用 DF-012 Server 协议、命令 lease、幂等和恢复能力，没有创建第二套命令存储；
- 复用 DF-011 Host offline 判断和状态收敛；
- 没有复制 DaFit 的 Appium WebDriver、动作、断言、Runner 或报告；
- 没有新增 Alcor Case、Run、RunAttempt、Result 或 Artifact 等业务对象；
- 没有直接暴露远程 Docker Socket；DF-014 的 Docker 操作仍只在 Host Agent 本机 Provider 内执行。

## 验收命令与结果

```powershell
go test -count=1 -v ./internal/agent ./cmd/device-host-agent
```

关键结果：

```text
PASS TestAgentStopsClaimingAndFinishesInflightCommands
PASS TestHTTPClientClaimUsesAgentTokenAndDecodesEnvelope
PASS TestHTTPClientReturnsAPIErrorForNonSuccessStatus
PASS internal/agent
cmd/device-host-agent [no test files]
```

```powershell
scripts/dev.ps1 -Task check
```

全量格式检查、静态检查、所有 Go 测试以及 Server/Agent 构建均通过。

## 验收结论

DF-013 已满足可运行 Agent、心跳与本机发现、长轮询、命令执行、并发限制、优雅退出和重启恢复边界，可以进入 DF-014 Docker Emulator Provider。
