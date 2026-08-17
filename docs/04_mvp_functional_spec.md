# Android 设备农场 MVP 功能方案

> 本文件是已经完成并归档的 Android 第一版功能基线。第二版多平台与 iOS 扩展见 `docs/08_ios_device_farm_v2_design.md` 和 ADR-0021；本文件中的“明确不做 iOS”仍对第一版实现有效，生产扩展只能按 DF-040～DF-046 逐项进入。

## 1. 交付目标

在不等待新版 Alcor 的情况下，先交付一套可独立运行、可通过 Web 控制、可自动测试的 Android 设备农场。首期使用 Linux KVM 宿主机上的 Docker Android Emulator；设备申请成功后返回明确的 UDID 和 Appium Endpoint。STF 原生 Web 页面保持独立受控访问，Console 不把 `remoteConnect` TCP 地址展示为浏览器入口。

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
- 单一默认逻辑设备池，当前默认自动维持一台 Emulator，并支持控制台统一调整固定目标后自动扩缩容；
- DaFit 端到端联调 Harness；
- 服务身份、Agent 身份、审计事件和敏感日志脱敏；
- OpenAPI、部署说明、故障处理和验收证据。
- Device Farm Console：设备总览、资源管理、人工预约、设备操作和设备域审计；不展示伪 STF Web 入口。

### 2.2 只预留扩展点

- USB Android 真机 Provider；
- 多宿主机调度；
- 多租户配额；
- 动态容量预测；固定目标扩缩容属于 MVP 设备域能力；
- S3、Kubernetes 和复杂调度策略。

### 2.3 明确不做

- Alcor Eval Console 的 Case、Dataset、Run、Result、评分、报告和发布门禁页面；
- Alcor 的 Case、Dataset、Target、Config、Run、RunAttempt；
- 评分、门禁、LLM 报告、ClickHouse 业务结果和 Supabase Artifact 管理；
- 复制 DaFit 页面对象、动作、断言、截图和报告实现；
- 自研 Appium、STF 远控协议或 Android Emulator；
- iOS；
- 在 Windows 本机假装完成 Linux KVM Emulator 验收。

## 3. 总体架构

```text
Device Farm Console（当前独立使用）
                         ↓ 同源安全访问
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
- 控制台不直连 PostgreSQL、Docker、RethinkDB、ADB 或 Appium，只调用 Device Farm Server；
- 浏览器不持有 Service Token 或 STF 管理 Token，写操作以服务端状态和审计结果为准。

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

镜像必须记录明确的 `docker_image` 运行引用、不可变 digest、API Level、ABI、分辨率和资源配置。后台选择 Image 后，validation、create 和 rebuild 必须使用该 Image 自身的运行引用；未经验证、缺少运行引用或已禁用的镜像不能进入设备池。

设备生命周期：

```text
provisioning → booting → ready → reserved → busy → ready
                       ↘ stopped
任一异常状态 → quarantined → rebuild → provisioning
stopped → deleted
```

另设健康状态 `unknown/healthy/degraded/unhealthy`，避免把生命周期和健康原因混成一个字段。所有状态转换必须由领域方法校验并写事件。

### 4.5 设备池与按资源动态扩缩容

设备池是预约和调度使用的逻辑分组，不等于自动创建模拟器的资源池。它保存默认/最大租期、最大并发、启停状态和设备成员关系。

MVP 只配置一个默认 Android 设备池。当前测试环境可以只运行一台，但代码和接口不得把一台作为固定上限：

- Pool 保存总目标、最小预热、最大并发和默认 Image；当前测试值可以为 1，迁移后不改代码即可提高；
- Android 16 是默认 Image，Android 13～15 是可选 Image，不按 Image 分别常驻一台；
- Controller 自动创建缺少的 Emulator，并在同一编排中登记 Device、Host Command 和 Pool membership；
- 自动创建必须经 Host Agent 执行 Docker Provider，不能由 Server 直连 Docker 或创建 Mock 设备；
- 多 Server 使用 PostgreSQL 行锁重新计算缺口，避免超额创建；
- 创建失败按有上限退避重试，设备只有通过 ADB、boot 和 Appium 健康检查后才计入 ready；
- 第二个 Reservation 在 Pool 并发、目标或 Host 实际资源不足时保持 pending/capacity unavailable，不因请求压力超建；
- Host Agent 上报实际 CPU、内存和 Docker 数据盘，Server 按每台有效规格、已有设备和在途命令计算剩余容量；可选 `device_slots` 只能作为安全上限，不能由目标数反向抬高；
- 控制台显示当前规格最多可新增台数以及 CPU、内存、磁盘中最先达到的限制，不要求管理员登录 Host 修改 Agent 配置；
- Controller 在目标降低时删除超出的最旧空闲 Emulator，保留最新实例；占用中、回收中或仍有其他 Pool membership 的设备不得被自动删除；
- 自动删除走持久化 delete Host Command 和 Agent/Docker Provider，成功后 Device 标记为 `deleted` 并保留历史，失败则隔离和告警；
- 管理员可对没有活动预约的 `quarantined/stopped` Device 发起人工删除；必须填写原因、携带幂等键并二次确认，复用同一 delete Host Command。成功后退出 Pool、清空 Endpoint 并标记 `deleted`，失败保持 `quarantined/unhealthy`；
- 缩容是最终一致的：占用中的最旧设备先等待释放，不能为立即达到数字而强制中断 Reservation。
- 管理员可对空闲 Emulator 选择 Android 13～16 Image 并修改 CPU、内存、数据盘、分辨率和 GPU 模式；该操作会清空设备数据并通过 Host Command 重装，成功前不改变当前 Image/规格，失败时恢复或隔离。

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
- 释放 STF claim、关闭 Device Session，并将 Device 直接恢复为 ready；
- 不执行重建、恢复出厂或数据卷清理；只有管理员显式 rebuild/reimage 才执行原有清空链路；
- 多实例运行时通过数据库锁避免重复回收。

Reconciler：

- 对比 PostgreSQL、Agent Discover 和 STF inventory；
- 修复悬挂 command/session/reservation；
- Agent 或 Server 重启后恢复状态；
- 设备不可恢复时隔离，不能继续分配。

### 4.8 STF Adapter

只封装：inventory、claim、release、remoteConnect 和健康检查。

- STF Token 只保存在 Server 配置；
- 可信服务端调用方可按技术契约获得短时 `remoteConnect` TCP Endpoint；浏览器 Console 不接收或展示该地址；
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

### 4.11 Device Farm Console

控制台至少提供：

- 设备总览：Host、Device、Pool、Reservation、健康和容量摘要；
- Image：列表、详情、创建/编辑、验证状态和池配置；
- Host：列表、心跳、容量、drain/undrain；
- Pool：列表、租期、Image、Device membership 和单一目标设备数；Pool 并发由启用 Image 目标自动同步；
- Device：列表、详情、连接状态、健康事件、restart、rebuild、quarantine/unquarantine，以及空闲 Emulator 的镜像和运行规格编辑；
- Reservation：创建人工预约、查看状态、续租、释放和当前连接信息；
- STF 原生远控：管理员从 Device 行一键创建精确设备短租约，无需再次输入 STF 账号密码，并在新标签页打开 STF 原生单设备控制页；
- 设备域审计：按资源查看操作人、原因、request ID、动作和时间。

控制台必须遵守：

- 页面状态来自 Server，不直接读取基础设施；刷新后必须与数据库真相一致；
- 非法状态下不展示可执行按钮，服务端仍必须再次校验；
- restart、rebuild、quarantine、unquarantine、drain、release 等危险操作必须二次确认并填写原因；
- 降低目标设备数必须二次确认并填写原因，页面说明实际删除可能等待占用结束；
- 所有错误显示稳定错误码、request ID 和是否可重试，不能只显示“操作失败”；
- 浏览器安全访问、CSRF、防缓存、内容安全策略和 Token 隔离由 DF-026 固化并验收；
- 不能实现 STF 的画面、触控、日志、文件和 ADB 协议，只复用 STF 原生页面和 Adapter。
- 远控点击挂断或检测到标签页关闭时必须结束 Reservation、释放 STF claim 并进入重建；浏览器崩溃、断网或整机关闭由心跳停止、短租约和 Reaper 兜底。
- 第一阶段只允许管理员同时操控一台设备；STF Web JWT 极短有效，签名 Secret、STF API Token 和 ADB TCP `remoteConnect` 地址不得进入浏览器持久存储或普通日志。
- Emulator 更换 Image、CPU、内存、数据盘或图形模式必须二次确认并明确提示 APK 和设备数据会被清空；当前配置只在目标实例通过 ADB、STF、Appium 全部检查后切换，失败时恢复旧配置一次，不能提前把数据库伪装成目标配置。

技术栈固定为 pnpm 11、Vite、React、TypeScript、Ant Design、React Router、TanStack Query 和 Orval；Orval 从设备 OpenAPI 生成 fetch client、类型和 Query hooks。测试使用 Vitest、React Testing Library、MSW 和 Playwright。生产构建嵌入现有 Go Server 并由 `/console/` 同源提供。

DF-026 将 `openapi/device-farm-v1.yaml` 的契约版本提升为 `1.2.0`；现有 Service Bearer 和 `/api/v1/device-*` 路径保持兼容，只增加 Console 会话、安全方案、精确成功响应和分页元数据。

DF-031 只向现有 `1.2.0` 契约增加管理员专用 `/console/api/v1/devices/{id}/remote-control*` 接口；既有 Alcor Adapter 的 Service Bearer、Reservation 和 Device 响应保持兼容，冻结哈希随有意变更更新。

ALCOR-001 将契约版本提升为 `1.9.0`，为 Alcor 受控网关增加 Service Bearer 版本的 `/api/v1/devices/{id}/remote-control*`。它与独立 Console 的管理员接口共同调用唯一 `remotecontrol.Service`、Reservation 和 STF JWT 链路；Alcor 浏览器只访问同源代理，不获得 Device Farm Service Token、独立 Console Cookie 或 STF Token。

DF-034 将契约版本提升为 `1.5.0`，新增 Device 当前/有效/待应用运行规格和 `POST /api/v1/devices/{id}/reimages`。该接口只编排设备域已有的 Host Command、Docker Provider、STF 和 Appium Adapter，不在 Console 重写底层能力。

### 4.12 其他资源状态

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
- `GET /api/v1/device-pools/:id/images`
- `PUT/DELETE /api/v1/device-pools/:id/images/:image_id`
- `POST/DELETE /api/v1/device-pools/:id/devices`
- `GET /api/v1/devices`
- `GET /api/v1/devices/:id`
- `POST /api/v1/devices/:id/restarts`
- `POST /api/v1/devices/:id/rebuilds`
- `POST /api/v1/devices/:id/reimages`
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

### 5.4 控制台访问

- 静态入口使用 `/console/` 或等价同源路径；
- 控制台资源操作复用 5.1 和 5.2 的设备 API，不创建第二套资源语义；
- `POST /console/api/v1/sessions` 创建短时会话；
- `GET /console/api/v1/me` 返回当前设备域用户和角色；
- `DELETE /console/api/v1/sessions/current` 撤销当前会话；
- `GET /api/v1/device-audit-events` 查询脱敏设备域审计；
- `GET /api/v1/devices/:id/health-events` 查询设备健康事件；
- `/api/v1/device-*` 同时接受 Service Bearer 或 Console Cookie；Console Principal 的 actor、client ID 和人工预约 owner 由服务端会话确定；
- 浏览器不能接收 Service Token、Agent Token、STF API Token、数据库 URL 或 Docker/ADB/Appium 内部地址。

## 6. 设备域数据模型

| 表 | MVP 关键字段 |
|---|---|
| `device_images` | id、name、docker_image、docker_digest、api_level、abi、resolution、resource_config、status |
| `device_hosts` | id、name、host_type、capabilities、capacity、used_capacity、status、draining、last_heartbeat_at |
| `device_host_commands` | id、host_id、command_type、payload、status、lease_token、lease_expires_at、attempts、idempotency_key、result |
| `device_pools` | id、name、default_lease_seconds、max_lease_seconds、total_target、min_ready、max_concurrency、default_image_id、status；Pool 总量是唯一扩缩容目标 |
| `device_pool_images` | pool_id、image_id、min_ready、max_instances、enabled；保留旧客户端兼容字段，只表示可选镜像目录，不再把各镜像目标相加 |
| `device_pool_devices` | pool_id、device_id、enabled |
| `devices` | id、host_id、image_id、device_kind、provider_type、provider_ref、serial、stf_serial、adb_endpoint、appium_endpoint、capabilities、lifecycle_status、health_status、health_reason |
| `device_reservations` | id、pool_id、device_id、owner_type、owner_id、requested_capabilities、status、idempotency_key、starts_at、expires_at、released_at、failure_code |
| `device_sessions` | id、reservation_id、device_id、status、started_at、ended_at、connection_metadata |
| `device_health_events` | id、device_id、source、event_type、severity、reason、payload、created_at |
| `device_audit_events` | id、actor_type、actor_id、action、resource_type、resource_id、request_id、summary、created_at |
| `device_idempotency_records` | client_id、scope、idempotency_key、request_hash、resource_type、resource_id、response_status、expires_at |
| `device_console_sessions` | id、token_hash、user_id、display_name、role、csrf_hash、created_at、last_seen_at、expires_at、revoked_at |

必须具有：

- active reservation 的 device 唯一约束；
- client + idempotency key 唯一约束；
- serial/provider_ref 必要唯一约束；
- 外键、时间检查和合法状态检查；
- migration up/down；
- 不对 Alcor RunAttempt 建跨库外键。
- Console 用户来自部署 Secret 文件，不建用户业务表；会话 Token 和 CSRF Token 只保存不可逆哈希，过期或撤销会话不能继续访问。

## 7. 错误分类

| 分类 | 示例错误码 | 是否可重试 |
|---|---|---|
| 参数/状态冲突 | `INVALID_ARGUMENT`、`INVALID_STATE_TRANSITION` | 否 |
| 容量不足 | `DEVICE_CAPACITY_UNAVAILABLE` | 是 |
| Host/Agent | `HOST_OFFLINE`、`AGENT_COMMAND_TIMEOUT` | 是 |
| Provider | `EMULATOR_CREATE_FAILED`、`KVM_UNAVAILABLE` | 视原因 |
| STF | `STF_CLAIM_FAILED`、`STF_RELEASE_FAILED`、`STF_REMOTE_CONNECT_FAILED` | 视具体 HTTP/网络错误 |
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
- `remoteConnect` 技术 Endpoint 短时有效且不能返回 STF 管理 Token；Console 不展示该 TCP 地址；
- 强制释放、隔离、解除隔离、重建必须记录 actor、reason 和 request ID；
- connection metadata 只保存必要 Endpoint，不持久化临时访问 Token。

## 9. MVP 完成定义

满足以下条件才算设备农场 MVP 完成：

1. Mock 环境可完整演示镜像、Host、Pool、Device、Reservation 全链路；
2. Linux KVM 环境能由 Agent 按控制台目标自动创建 Android 16 Docker Emulator，并在降低目标后安全删除超额实例；当前默认和基础验收仍为一台；
3. 单台模拟器只能产生一个 active reservation，第二个并发预约不能双占或突破容量；
4. 每台设备可建立独立 Appium Session；
5. STF 可看屏、claim、release，且不作为数据库真相；
6. DaFit 冒烟用例能够申请设备、运行、收集报告并释放；
7. 超时、Agent 离线、STF 失败和 Appium 失败能够回收或隔离；
8. Server/Agent 重启后两分钟内状态收敛；
9. 重建后无法读取上一次任务 App 数据；
10. OpenAPI、migration、部署说明、测试报告和回滚步骤齐全。
11. 用户可通过 Device Farm Console 完成资源查看、人工预约、续租/释放和受控设备操作，且浏览器无内部 Token、无伪 STF Web 入口；STF claim/release 和原生设备可见性在独立服务中真实通过。
