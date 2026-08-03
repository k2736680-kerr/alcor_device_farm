# Android 设备农场 MVP 功能方案

## 1. 交付目标

在不等待新版 Alcor 的情况下，先交付一套可独立运行、可自动测试的 Android 设备农场。首期使用 Linux KVM 宿主机上的 Docker Android Emulator；设备申请成功后返回明确的 UDID、Appium Endpoint 和受控 STF 远控入口。

未来新版 Alcor 的独立 Worker 只需实现 Device Farm Adapter，便可以 RunAttempt 身份申请、使用和释放设备，不需要改造设备农场核心。

## 2. MVP 范围

### 2.1 必须完成

- 一个设备农场 Server；
- 一个部署在 Linux KVM 宿主机上的 Host Agent；
- PostgreSQL 设备域数据库；
- Mock Provider，用于没有 Docker、STF 和设备时开发；
- Docker Emulator Provider；
- 统一 Device 模型和生命周期状态机；
- Android 镜像、宿主机、设备池、设备管理；
- 异步设备预约、续租、释放和过期回收；
- Scheduler、Reaper、Reconciler；
- STF inventory、claim、release、remoteConnect Adapter；
- Appium Endpoint、端口和健康检查；
- 单一默认逻辑设备池，参数化自动维持最多两台 Emulator；
- DaFit 端到端联调 Harness；
- 服务身份、Agent 身份、审计事件和敏感日志脱敏；
- OpenAPI、部署说明、故障处理和验收证据。

### 2.2 只预留扩展点

- USB Android 真机 Provider；
- 多宿主机调度；
- 多租户配额；
- 动态容量预测；
- S3、Kubernetes 和复杂调度策略。

### 2.3 明确不做

- Eval Console 或独立设备管理前端；
- Alcor 的 Case、Dataset、Target、Config、Run、RunAttempt；
- 评分、门禁、LLM 报告、ClickHouse 业务结果和 Supabase Artifact 管理；
- 复制 DaFit 页面对象、动作、断言、截图和报告实现；
- 自研 Appium、STF 远控协议或 Android Emulator；
- iOS；
- 在 Windows 本机假装完成 Linux KVM Emulator 验收。

## 3. 总体架构

```text
DaFit Harness（当前） / Alcor Worker + Device Farm Adapter（未来）
                         ↓ /api/v1/device-*
                  Device Farm Server
        ┌────────────────┼────────────────┐
        ↓                ↓                ↓
   PostgreSQL       STF Adapter      Appium Adapter
        ↓                ↓                ↓
 Scheduler/Reaper   STF+RethinkDB    Endpoint/Health
 Reconciler
        ↓ /internal/v1
   Device Host Agent
        ↓
 Docker Engine + KVM
        ↓
 Android Emulator 容器 1...N
```

技术约束：

- Server、Agent 和设备域核心使用 Go，保持单一 Go module；
- DaFit 继续使用原 Python 项目；
- PostgreSQL 同时承担设备状态、租约、幂等和命令队列，不引入 Redis、Kafka、Temporal；
- Server 不直接访问远程 Docker Socket，Docker 操作只能由 Host Agent 执行；
- Agent 主动访问 Server 领取命令，不要求 Server 反向进入宿主机；
- STF RethinkDB 不作为设备预约真相源。

## 4. 模块与功能

### 4.1 Device Farm Server

负责：

- `/api/v1/device-*` 北向接口；
- `/internal/v1` Agent 接口；
- 参数校验、幂等、统一响应、关联 ID；
- 设备域 service/repository；
- Scheduler、Reaper、Reconciler 的运行和主节点互斥；
- 服务身份和 Agent 身份校验；
- 技术审计事件。

不负责：执行 DaFit 页面步骤、生成业务报告、保存 Alcor RunResult。

### 4.2 Host Agent

负责：

- 注册、心跳、容量和环境信息上报；
- 长轮询领取具备租约的 Host Command；
- 幂等执行 create/start/stop/restart/rebuild/delete/inspect；
- 调用 Docker Emulator Provider；
- 上报 ADB、boot completed、Appium 和容器健康；
- 命令完成、失败、超时结果上报；
- 本机恢复后重新发现已有设备。

Agent 不保存业务用户、Run、Dataset 或 Supabase 密钥。

### 4.3 Provider

统一接口至少包含：

```text
Discover
Create
Start
Stop
Restart
Rebuild
Delete
InspectHealth
GetConnectionInfo
```

- `MockProvider`：内存或测试数据库驱动，支持成功、超时、离线和失败注入；
- `DockerEmulatorProvider`：管理容器化 Android Emulator，每个设备拥有独立 serial、ADB/Appium 端口和数据目录；
- `USBPhysicalDeviceProvider`：本期只定义接口兼容性，不实现真机操作。

### 4.4 设备与镜像

镜像必须记录不可变 digest、API Level、ABI、分辨率和资源配置。未经验证或已禁用镜像不能进入设备池。

设备生命周期：

```text
provisioning → booting → ready → reserved → busy → recycling → ready
                       ↘ stopped
任一异常状态 → quarantined → rebuild → provisioning
stopped → deleted
```

另设健康状态 `unknown/healthy/degraded/unhealthy`，避免把生命周期和健康原因混成一个字段。所有状态转换必须由领域方法校验并写事件。

### 4.5 设备池与固定目标自动补齐

设备池是预约和调度使用的逻辑分组，不等于自动创建模拟器的资源池。它保存默认/最大租期、最大并发、启停状态和设备成员关系。

MVP 只配置一个默认 Android 设备池：

- `max_concurrency=2`；
- `device_pool_images.min_ready=2/max_instances=2`；
- Controller 自动创建缺少的 Emulator，并在同一编排中登记 Device、Host Command 和 Pool membership；
- 自动创建必须经 Host Agent 执行 Docker Provider，不能由 Server 直连 Docker 或创建 Mock 设备；
- 多 Server 使用 PostgreSQL 行锁重新计算缺口，避免超额创建；
- 创建失败按有上限退避重试，设备只有通过 ADB、boot 和 Appium 健康检查后才计入 ready；
- 两台设备均占用时第三个 Reservation 保持 pending/capacity unavailable，不因请求压力突破 `max_instances`；
- Controller 不自动删除设备；降低目标后只停止补充，通过 drain/人工删除缩容。

后续接入 USB 真机时，由 Agent 发现并显式加入默认池；若业务需要明确选择真机，则新增一个逻辑真机池。真机不参与 Emulator 自动创建，但继续复用统一 Device、Reservation、Scheduler 和 Provider 模型。

### 4.6 预约与调度

预约状态：

```text
pending → active → released
pending → failed
active  → expired
active  → force_released
```

创建预约时保存：

- `owner_type`：`run_attempt`（新版 Alcor 自动执行）、`manual`（人工调试）或 `test_run`（DaFit 联调）；
- `owner_id`：UUID/ULID 字符串；
- `pool_id`；
- required capabilities；
- lease seconds；
- `Idempotency-Key`；
- Run/Attempt/Trace 关联标识。

Scheduler 按以下顺序执行：

1. PostgreSQL 领取 pending reservation；
2. 使用 `FOR UPDATE SKIP LOCKED` 选择满足能力的 ready device；
3. 在事务内建立唯一 active reservation 并将设备改为 reserved；
4. 事务外调用 STF claim 和连接健康检查；
5. 成功后创建 Device Session、将设备改为 busy 并激活连接信息；
6. 失败时执行补偿，关闭预约并按错误类型恢复或隔离设备。

同一个设备任何时刻最多一个 active reservation。相同客户端和幂等键必须返回同一预约，不得重复占用。

### 4.7 Reaper 与 Reconciler

Reaper：

- 回收超过 `expires_at + grace_period` 的预约；
- 释放 STF claim；
- 关闭 Device Session；
- 将设备送入 recycling；
- 多实例运行时通过数据库锁避免重复回收。

Reconciler：

- 对比 PostgreSQL、Agent Discover 和 STF inventory；
- 修复悬挂 command/session/reservation；
- Agent 或 Server 重启后恢复状态；
- 设备不可恢复时隔离，不能继续分配。

### 4.8 STF Adapter

只封装：inventory、claim、release、remoteConnect 和健康检查。

- STF Token 只保存在 Server 配置；
- 浏览器或 Alcor 只获得短时远控入口；
- claim 失败不能继续返回 active reservation；
- release 失败进入重试和审计，不得直接忘记占用。

### 4.9 Appium Adapter

只负责：

- 记录每台设备的 Endpoint；
- 区分容器外 ADB `serial/adb_endpoint` 和容器内 `appium_udid`，并把两者固化到 Device Session 连接快照；
- 检查 `/status` 或等价健康接口；
- 验证端口隔离和可达性；
- 将 Endpoint 随 active reservation 返回。

业务 WebDriver Session、页面动作、断言、截图和报告仍由 DaFit 或未来 Alcor Android Executor 负责。

### 4.10 DaFit Harness

执行顺序：

1. 创建 reservation；
2. 轮询到 active；
3. 注入明确 UDID、Appium Endpoint、独立报告目录；
4. 调用 DaFit 原有入口；
5. 收集原有 HTML/JSON 报告路径；
6. 无论成功、失败、取消或超时都释放预约；
7. 验证重建后的设备不保留上一次 App 数据。

### 4.11 其他资源状态

| 资源 | 状态 |
|---|---|
| Device Image | `draft / validating / ready / failed / disabled` |
| Device Host | `online / offline / draining / maintenance` |
| Device Pool | `active / disabled` |
| Host Command | `pending / leased / succeeded / failed / timed_out / canceled` |
| Device Session | `starting / active / closing / closed / failed` |

状态不得通过任意字符串直接更新。禁用 Image/Pool、Host draining 和 Session 关闭都必须保留历史记录，不使用物理删除清理审计链。

## 5. 北向 API MVP

统一响应：

```json
{
  "request_id": "req_...",
  "data": {},
  "error": null
}
```

错误时 `data=null`，`error` 至少包含稳定 `code`、可读 `message`、可选 `details` 和 `retryable`。

### 5.1 管理资源

- `POST/GET /api/v1/device-images`
- `GET/PUT /api/v1/device-images/:id`
- `POST /api/v1/device-images/:id/validations`
- `POST/GET /api/v1/device-hosts`
- `GET/PUT /api/v1/device-hosts/:id`
- `POST /api/v1/device-hosts/:id/drains`
- `DELETE /api/v1/device-hosts/:id/drains`
- `POST/GET /api/v1/device-pools`
- `GET/PUT /api/v1/device-pools/:id`
- `POST/DELETE /api/v1/device-pools/:id/devices`
- `GET /api/v1/devices`
- `GET /api/v1/devices/:id`
- `POST /api/v1/devices/:id/restarts`
- `POST /api/v1/devices/:id/rebuilds`
- `POST /api/v1/devices/:id/quarantines`
- `DELETE /api/v1/devices/:id/quarantines`

### 5.2 预约资源

- `POST/GET /api/v1/device-reservations`
- `GET /api/v1/device-reservations/:id`
- `POST /api/v1/device-reservations/:id/extensions`
- `POST /api/v1/device-reservations/:id/releases`
- `POST /api/v1/device-reservations/:id/remote-sessions`

### 5.3 Agent 内部资源

- `POST /internal/v1/device-hosts/:id/heartbeats`
- `POST /internal/v1/device-hosts/:id/commands/claims`
- `POST /internal/v1/device-host-commands/:id/completions`
- `POST /internal/v1/devices/:id/health-events`

所有北向请求支持 `Idempotency-Key`、`X-Request-Id`、`X-Eval-Run-Id`、`X-Eval-Attempt-Id` 和 `traceparent`。新版 Alcor 自动执行必须使用 `owner_type=run_attempt`；当前 DaFit 使用 `test_run` 和测试 UUID；人工调试使用 `manual` 和操作记录，字段格式保持一致。

## 6. 设备域数据模型

| 表 | MVP 关键字段 |
|---|---|
| `device_images` | id、name、docker_digest、api_level、abi、resolution、resource_config、status |
| `device_hosts` | id、name、host_type、capabilities、capacity、used_capacity、status、draining、last_heartbeat_at |
| `device_host_commands` | id、host_id、command_type、payload、status、lease_token、lease_expires_at、attempts、idempotency_key、result |
| `device_pools` | id、name、default_lease_seconds、max_lease_seconds、status |
| `device_pool_images` | pool_id、image_id、min_ready、max_instances、enabled；驱动固定目标自动补齐 Emulator |
| `device_pool_devices` | pool_id、device_id、enabled |
| `devices` | id、host_id、image_id、device_kind、provider_type、provider_ref、serial、stf_serial、adb_endpoint、appium_endpoint、capabilities、lifecycle_status、health_status、health_reason |
| `device_reservations` | id、pool_id、device_id、owner_type、owner_id、requested_capabilities、status、idempotency_key、starts_at、expires_at、released_at、failure_code |
| `device_sessions` | id、reservation_id、device_id、status、started_at、ended_at、connection_metadata |
| `device_health_events` | id、device_id、source、event_type、severity、reason、payload、created_at |
| `device_audit_events` | id、actor_type、actor_id、action、resource_type、resource_id、request_id、summary、created_at |
| `device_idempotency_records` | client_id、scope、idempotency_key、request_hash、resource_type、resource_id、response_status、expires_at |

必须具有：

- active reservation 的 device 唯一约束；
- client + idempotency key 唯一约束；
- serial/provider_ref 必要唯一约束；
- 外键、时间检查和合法状态检查；
- migration up/down；
- 不对 Alcor RunAttempt 建跨库外键。

## 7. 错误分类

| 分类 | 示例错误码 | 是否可重试 |
|---|---|---|
| 参数/状态冲突 | `INVALID_ARGUMENT`、`INVALID_STATE_TRANSITION` | 否 |
| 容量不足 | `DEVICE_CAPACITY_UNAVAILABLE` | 是 |
| Host/Agent | `HOST_OFFLINE`、`AGENT_COMMAND_TIMEOUT` | 是 |
| Provider | `EMULATOR_CREATE_FAILED`、`KVM_UNAVAILABLE` | 视原因 |
| STF | `STF_CLAIM_FAILED`、`STF_RELEASE_FAILED` | 是 |
| Appium | `APPIUM_UNHEALTHY` | 是 |
| 设备健康 | `DEVICE_BOOT_TIMEOUT`、`DEVICE_QUARANTINED` | 视原因 |
| 租约 | `RESERVATION_EXPIRED`、`LEASE_EXTENSION_REJECTED` | 否 |
| 权限 | `UNAUTHORIZED`、`FORBIDDEN` | 否 |

错误必须区分用户/业务不可重试、容量稍后重试和基础设施故障，方便新版 Alcor 将其映射为 `failed` 或 `infra_failed`。

## 8. 最小安全要求

- 北向服务 Token 与 Agent Token 分离；
- 数据库、STF、Docker、Supabase 或 Target 密钥不得出现在 API、日志和审计摘要；
- Agent Token 只保存 hash 或由部署配置注入；
- Docker Socket 只在 Agent 宿主机本地使用；
- 远控入口短时有效，不能返回 STF 管理 Token；
- 强制释放、隔离、解除隔离、重建必须记录 actor、reason 和 request ID；
- connection metadata 只保存必要 Endpoint，不持久化临时访问 Token。

## 9. MVP 完成定义

满足以下条件才算设备农场 MVP 完成：

1. Mock 环境可完整演示镜像、Host、Pool、Device、Reservation 全链路；
2. Linux KVM 环境能由 Agent 创建至少两台 Docker Emulator；
3. 两台模拟器可以并发预约且不会双占；
4. 每台设备可建立独立 Appium Session；
5. STF 可看屏、claim、release，且不作为数据库真相；
6. DaFit 冒烟用例能够申请设备、运行、收集报告并释放；
7. 超时、Agent 离线、STF 失败和 Appium 失败能够回收或隔离；
8. Server/Agent 重启后两分钟内状态收敛；
9. 重建后无法读取上一次任务 App 数据；
10. OpenAPI、migration、部署说明、测试报告和回滚步骤齐全。
