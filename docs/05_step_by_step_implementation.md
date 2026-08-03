# 设备农场逐步实施清单

## 1. 使用方法

本文件是后续开发的唯一任务顺序。每次只实施一个编号，例如：

```text
开始 DF-001
继续下一步
验收 DF-009
```

任务状态使用 `pending / in_progress / completed / blocked / waiting_external`。完成任务时必须同时更新本文件状态，并把关键测试输出、截图或报告放入 `docs/evidence/DF-xxx/`。

每个 DF 任务验收通过后必须单独 Git commit。提交说明统一使用简洁中文，直接说明完成内容，例如“建立 Go 工程骨架”“加入预约和调度能力”。

任何任务都不能通过提前实现新版 Alcor 的 Run、Result、Artifact 或前端来“顺便完成”。

## 2. 全部任务状态

| 编号 | 任务 | 状态 | 前置任务 |
|---|---|---|---|
| DF-000 | 环境与依赖盘点 | completed | 无 |
| DF-001 | Go 工程骨架与本地开发入口 | completed | DF-000 |
| DF-002 | 配置、日志、统一响应和关联 ID | completed | DF-001 |
| DF-003 | OpenAPI 和服务/Agent 认证骨架 | completed | DF-002 |
| DF-004 | 设备域 PostgreSQL migration | completed | DF-001 |
| DF-005 | 领域模型和状态机 | completed | DF-004 |
| DF-006 | Repository、事务和幂等基础 | completed | DF-004、DF-005 |
| DF-007 | Mock Provider 和故障注入 | completed | DF-005 |
| DF-008 | Image、Host、Pool、Device API | completed | DF-003、DF-006、DF-007 |
| DF-009 | Reservation API 和 Scheduler | completed | DF-008 |
| DF-010 | 续租、释放和 Reaper | completed | DF-009 |
| DF-011 | Reconciler、健康事件和隔离 | completed | DF-010 |
| DF-012 | Agent 内部协议和 Host Command | completed | DF-003、DF-006 |
| DF-013 | Host Agent 核心程序 | completed | DF-012、DF-007 |
| DF-014 | Docker Emulator Provider | blocked | DF-013 |
| DF-015 | Appium Endpoint 和健康 Adapter | blocked | DF-014 |
| DF-016 | 镜像验证和固定目标自动补齐 | blocked | DF-011、DF-014、DF-015 |
| DF-017 | STF 与 RethinkDB 部署 | blocked | DF-014 |
| DF-018 | STF Adapter 和远控入口 | blocked | DF-009、DF-017 |
| DF-019 | DaFit Farm 运行适配 | blocked | DF-015 |
| DF-020 | DaFit 端到端 Harness | blocked | DF-018、DF-019 |
| DF-021 | 故障恢复、清理和数据隔离 | blocked | DF-020 |
| DF-022 | 权限、审计和敏感数据加固 | blocked | DF-018、DF-021 |
| DF-023 | 指标、部署、运维和回滚手册 | blocked | DF-022 |
| DF-024 | MVP 全量验收 | blocked | DF-023 |
| DF-025 | 新版 Alcor Adapter 契约包 | pending | DF-024 |
| ALCOR-001 | 新版 Alcor 真实接口联调 | waiting_external | DF-025、新版 Alcor OpenAPI |

## 3. 阶段 A：工程和契约基础

### DF-000 环境与依赖盘点

实施：

- 确认本项目和 DaFit 路径、分支及未提交改动；
- 确认 Go、Docker、PostgreSQL、ADB、Appium、STF 所需版本来源；
- 确认是否已有 Linux KVM 宿主机；
- 记录端口、网络、CPU、内存、磁盘和虚拟化能力；
- 任何 Token 只记录配置项名称，不写真实值。

产出：`docs/environment_inventory.md` 和脱敏的环境检查输出。

验收：可以明确回答哪些测试可在 Windows/Mock 完成，哪些必须等待 Linux KVM；没有修改任何业务代码或泄露密钥。

### DF-001 Go 工程骨架与本地开发入口

实施：建立单一 Go module、Server/Agent 两个 cmd、internal 分层、Makefile/PowerShell 入口、基础单元测试和 CI 命令。现有空目录在确认无代码后统一整理。

产出：可编译的 Server、Agent，标准目录和开发说明。

验收：全新环境执行项目规定的一条构建命令和一条测试命令均成功；Server/Agent 可显示版本并正常退出；`master` 分支无重复 Go module。

### DF-002 配置、日志、统一响应和关联 ID

实施：实现 YAML + 环境变量覆盖、配置校验、结构化日志、request ID、中间件、统一 `data/error` 响应和关联 Header 透传。

产出：配置样例、错误码基础包、HTTP 中间件。

验收：缺少必填配置时启动失败且提示明确；API 响应含 request ID；日志可检索 Run/Attempt/Trace ID；测试证明密码、Token 不会被序列化进日志。

### DF-003 OpenAPI 和服务/Agent 认证骨架

实施：定义全部 MVP 路径、请求/响应 Schema、错误码、服务 Token 与 Agent Token 两类认证。

产出：`openapi/device-farm-v1.yaml`、认证中间件和契约测试。

验收：OpenAPI 可通过校验工具；未认证返回 401，权限不足返回 403；示例请求可以生成客户端或通过契约测试；没有旧 `/eval-tasks` 依赖。

## 4. 阶段 B：数据和 Mock 控制面

### DF-004 设备域 PostgreSQL migration

实施：创建 04 方案中的设备域表、外键、检查约束、唯一索引和 up/down migration。

产出：独立编号的 migration 和 ER 说明。

验收：空库 `up → down → up` 成功；重复 serial、重复幂等键和设备双 active reservation 被数据库拒绝；数据库中不存在 Alcor Run/Result 表。

### DF-005 领域模型和状态机

实施：实现 Image、Host、Device、Pool、Reservation、Session、Command 领域对象和合法状态转换。

产出：领域包、状态图和表驱动测试。

验收：每条合法转换有成功测试，每条禁止转换有失败测试；不能绕过领域方法把 quarantined 设备改为 ready；生命周期和健康状态分离。

### DF-006 Repository、事务和幂等基础

实施：实现 repository 接口、事务封装、行锁、SKIP LOCKED、幂等记录和数据库时钟使用规则。

产出：repository、集成测试数据库入口。

验收：并发领取同一 command/reservation 只有一个成功；事务回滚后不留半条预约；相同幂等键返回同一资源；测试不依赖进程内 mutex 保证数据库正确性。

### DF-007 Mock Provider 和故障注入

实施：实现可控制创建时间、启动、离线、超时、Appium 失败和删除失败的 Mock Provider。

产出：Mock Provider、测试场景配置和示例设备。

验收：无需 Docker/STF/ADB 即可生成 ready 设备；每种主要故障可确定性复现；测试结束不残留 goroutine 或数据库占用。

### DF-008 Image、Host、Pool、Device API

实施：实现资源创建、查询、详情、drain、restart、rebuild、quarantine 等 API 和 service。

产出：可使用 Mock Provider 的完整管理 API。

验收：OpenAPI 中每个资源接口有成功、参数错误、权限错误和非法状态测试；draining Host 不接新设备；quarantined Device 不参与调度。

## 5. 阶段 C：预约和状态收敛

### DF-009 Reservation API 和 Scheduler

实施：实现 pending reservation、能力匹配、FIFO 领取、SKIP LOCKED 分配、active 唯一约束和 Session 创建。

产出：Reservation API、Scheduler 和并发测试。

验收：100 个并发请求竞争 2 台设备时无双占、无重复 active reservation；相同幂等键只创建一个预约；不满足能力时保持 pending 或按策略失败，不能错配设备。

### DF-010 续租、释放和 Reaper

实施：实现 extension、主动 release、强制 release、grace period 和数据库锁保护的 Reaper。

产出：租约 service、后台 Reaper、时间可控测试。

验收：合法续租更新 expires_at；超过最大租期被拒绝；过期预约在目标时间内关闭；并发 release 幂等；两台 Reaper 同时运行只回收一次。

### DF-011 Reconciler、健康事件和隔离

实施：实现 DB/Provider/STF 状态比对、健康事件、启动超时、失败计数、隔离和人工恢复。

产出：Reconciler、health event API、恢复策略。

验收：模拟 DB ready 但 Provider 不存在、Agent 离线、STF 不可见、设备重复失败等场景均收敛到明确状态；不可恢复设备不会再分配。

## 6. 阶段 D：Host Agent 和 Docker Emulator

### DF-012 Agent 内部协议和 Host Command

实施：实现 heartbeat、command claim、command completion、命令租约、幂等键、超时和重试上限。

产出：内部 API、Host Command repository/service、契约测试。

验收：Agent 重复领取同一命令不会重复执行；命令租约过期可安全重领；旧 completion 不得覆盖新 attempt；Agent 无法调用北向管理 API。

### DF-013 Host Agent 核心程序

实施：实现注册、心跳、长轮询、命令执行框架、并发限制、优雅退出和本机设备发现。

产出：可用 Mock Provider 运行的 Agent。

验收：Agent/Server 任一重启后命令不丢失；退出时停止领取新命令并完成或归还已有命令；心跳超时后 Host 自动 offline。

### DF-014 Docker Emulator Provider

实施：在 Linux KVM 上复用固定版本的 Android Emulator Container 基础，完成 create/start/stop/restart/rebuild/delete/discover 和资源限制。

产出：Provider、镜像/容器命名规则、端口分配和 Linux 部署脚本。

验收：同一 Host 能创建至少两台 serial/端口互不冲突的模拟器；ADB online、boot completed 成功；删除后容器、网络和临时数据清理；无 KVM 时明确失败，不能静默降级冒充通过。

### DF-015 Appium Endpoint 和健康 Adapter

实施：为每台 Emulator 建立独立 Endpoint，完成端口分配、状态检查和连接信息返回。

产出：Appium Adapter 和健康探针。

验收：两台设备可同时创建独立 Appium Session；错误 UDID 不能连接到其他设备；Appium 不健康时设备不得变为 ready。

### DF-016 镜像验证和固定目标自动补齐

实施：实现 digest 验证和镜像 validation；建立 `max_concurrency=2` 的默认逻辑设备池；使用 PostgreSQL 行锁按 `min_ready/max_instances` 计算缺口，原子登记 provisioning Device、Pool membership 和 Host Command，由 Agent/Docker 自动创建并在 Appium 健康后转为 ready。失败必须退避且可补偿，不实现负载预测或自动删除缩容。

产出：镜像验证任务、固定目标 Controller、Host Command 编排、默认池配置和两设备容量检查。

验收：未验证镜像不能启动设备；配置 `min_ready=2/max_instances=2` 后自动创建并加入两台 Emulator；删除或隔离一台后自动补回；两个 Controller 并发运行不超建；Docker/KVM/Appium 持续失败时有退避且不形成命令风暴；第三个并发预约保持 pending/capacity unavailable；降低目标不会自动删除在用设备；真机不被自动创建。

## 7. 阶段 E：STF 和真实执行

### DF-017 STF 与 RethinkDB 部署

实施：提供固定版本和配置的内网部署，完成 Emulator 发现、日志和远控基础验证。

产出：Docker Compose、配置样例和健康检查。

验收：至少两台 Emulator 在 STF 显示正确 serial 和 ready 状态；重启 STF 不改变设备农场 reservation 真相；管理 Token 不暴露给浏览器。

### DF-018 STF Adapter 和远控入口

实施：实现 inventory、claim、release、remoteConnect、超时、错误分类和短时入口。

产出：STF Adapter、预约编排补偿和测试。

验收：reservation active 前完成 claim；claim 失败时不返回可用设备；release 可重试；短时远控入口过期后失效；不同预约不能获得对方远控入口。

### DF-019 DaFit Farm 运行适配

实施位置：`dafit_auto_platform` 的独立 feature 分支。只增加外部 UDID、Appium Endpoint、报告目录和 Farm 模式，保持本地模式不变。

产出：DaFit 薄适配和回归测试。

验收：现有 collect-only 仍发现当前主线 158 个执行实例；原本地入口行为不变；Farm 模式缺少明确 UDID 时直接失败，不能自动挑第一台设备；不复制页面、动作、断言和报告模块。

### DF-020 DaFit 端到端 Harness

实施：实现申请、轮询、注入、运行、报告收集和 finally release。

产出：一条可重复执行的端到端命令和证据目录。

验收：一条 DaFit 冒烟用例无人值守完成；成功和故意失败两种场景都释放设备；报告属于对应运行目录；Harness 被中断后由 Reaper 兜底回收。

### DF-021 故障恢复、清理和数据隔离

实施：覆盖 Agent 离线、Server 重启、STF 失败、Appium 失败、Emulator boot timeout、命令超时和清理失败。

产出：故障测试套件、补偿矩阵和隔离策略。

验收：每类故障最终进入 released/expired/failed/quarantined 中的明确状态；无永久 reserved 悬挂；重建后无法读取上一任务安装的 App 数据、缓存和外部存储测试文件。

## 8. 阶段 F：交付和 Alcor 准备

### DF-022 权限、审计和敏感数据加固

实施：服务/Agent Token 轮换、审计字段、强制操作原因、日志脱敏和最小权限。

产出：安全配置、审计 API/查询方式和测试。

验收：强制释放/隔离/重建无 reason 时拒绝；Token 不出现在日志、响应、数据库普通字段和测试证据；过期 Agent Token 无法续用。

### DF-023 指标、部署、运维和回滚手册

实施：增加服务、Scheduler、设备、Agent、预约和错误指标；完善 Compose/服务配置、备份、升级、回滚和故障手册。

产出：部署包、Dashboard 指标清单、runbook、rollback 文档。

验收：新环境按文档可部署；配置校验失败不会带病启动；migration 和服务版本可按回滚手册恢复；关键告警可以通过故障注入触发。

### DF-024 MVP 全量验收

实施：执行 `07_acceptance_test_plan.md` 的全部 P0/P1 用例并整理证据。

产出：签字版验收报告、已知问题和版本清单。

验收：所有 P0 必须通过；P1 未通过项必须有明确风险接受和修复计划；没有未解释的资源、容器、预约或 Token 残留。

### DF-025 新版 Alcor Adapter 契约包

实施：冻结设备 OpenAPI、示例客户端、Mock Server、错误映射、RunAttempt/Reservation 时序和契约测试套件。

产出：Alcor 团队可以独立使用的 Adapter 接入包。

验收：不启动真实设备即可用 Mock 完成申请、active、续租、release、capacity unavailable、infra failure 流程；契约只使用 Case/Run/RunAttempt 新语义，不出现旧 Eval Task。

### ALCOR-001 新版 Alcor 真实接口联调

该任务必须等待新版 Alcor 实际分支和 OpenAPI，不能提前标记完成。

实施：核对认证、RunAttempt、取消、ArtifactStore 和关联 Header；实现 Worker Device Farm Adapter；执行双方契约和真实 DaFit/Android 冒烟。

验收：Eval Console 创建 Run 后，Worker 自动申请设备、执行、写 ClickHouse/Supabase、释放设备；RunAttempt 与 Device Session 可双向追溯；失败正确映射为 failed 或 infra_failed。

## 9. 单任务完成定义

每个 DF 任务只有同时满足以下条件才能改为 `completed`：

1. 代码、配置、migration 或文档产出齐全；
2. 单元/集成/契约测试按任务要求通过；
3. 新增公共接口已更新 OpenAPI；
4. 新增状态或表已更新功能方案和数据说明；
5. 没有复制 Alcor、DaFit、STF 或 Appium 已有能力；
6. 没有引入真实密钥和机器专用路径；
7. `docs/evidence/DF-xxx/` 有可复核证据；
8. 本任务变更不破坏此前已完成任务；
9. 已更新任务状态和已知问题；
10. 已使用简洁中文提交本任务 Git commit。
