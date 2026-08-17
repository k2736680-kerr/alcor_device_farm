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

任何任务都不能通过提前实现新版 Alcor 的 Run、Result、Artifact 或评估业务前端来“顺便完成”。Device Farm Console 只按 DF-026～DF-028 实现设备域页面。

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
| DF-014 | Docker Emulator Provider | completed | DF-013 |
| DF-015 | Appium Endpoint 和健康 Adapter | completed | DF-014 |
| DF-016 | 镜像验证和固定目标自动补齐 | completed | DF-011、DF-014、DF-015 |
| DF-017 | STF 与 RethinkDB 部署 | completed | DF-014 |
| DF-018 | STF Adapter 和远控入口 | completed | DF-009、DF-017 |
| DF-019 | DaFit Farm 运行适配 | completed | DF-015 |
| DF-020 | DaFit 端到端 Harness | completed | DF-018、DF-019 |
| DF-021 | 故障恢复、清理和数据隔离 | completed | DF-020 |
| DF-022 | 权限、审计和敏感数据加固 | completed | DF-018、DF-021 |
| DF-023 | 指标、部署、运维和回滚手册 | completed | DF-022 |
| DF-024 | MVP 全量验收 | completed | DF-023 |
| DF-025 | 新版 Alcor Adapter 契约包 | completed | DF-024 |
| DF-026 | Device Farm Console 工程和只读页面 | completed | DF-003、DF-004、DF-008、DF-025 |
| DF-027 | 设备操作和人工预约页面 | completed | DF-009、DF-010、DF-011、DF-026 |
| DF-028 | 控制台部署、安全和真实 Web 验收 | completed | DF-017～DF-024、DF-027 |
| DF-029 | 控制台统一容量和自动安全缩容 | completed | DF-016、DF-023、DF-028 |
| DF-030 | 隔离设备受控人工删除 | completed | DF-012、DF-022、DF-029 |
| DF-031 | 管理员设备远程控制 | completed | DF-010、DF-017、DF-018、DF-028 |
| DF-032 | 动态 Host 容量和设备运行规格 | completed | DF-014、DF-016、DF-029、DF-031 |
| DF-033 | Pool 总目标和默认镜像 | completed | DF-032 |
| DF-034 | 设备规格编辑和受控重装 | completed | DF-033 |
| DF-035 | Android 13～16 镜像目录和真实多规格验收 | completed | DF-034 |
| DF-036 | 旧镜像受控停用和可用镜像选择 | completed | DF-035 |
| DF-037 | Phone 硬件模板和受控模拟器创建向导 | completed | DF-036 |
| DF-038 | 长期设备、基础设备扩容和 Android Studio 式创建流程 | completed | DF-037 |
| ALCOR-001 | 新版 Alcor 真实接口联调与统一入口 | completed | DF-028、新版 Alcor 实际 `test` 分支 |
| DF-039 | 第二版多平台宿主机与 iOS 接入设计 | in_progress | DF-038、ALCOR-001、Android 第一版归档基线 |
| DF-040 | 平台中立设备域模型与契约 | pending | DF-039 |
| DF-041 | macOS Host Agent 与 Appium Device Farm Adapter | pending | DF-040 |
| DF-042 | Reservation 绑定的 iOS Session Fence | pending | DF-041 |
| DF-043 | iOS Simulator 固定库存接入 | pending | DF-042 |
| DF-044 | iOS 真机、WDA 签名与健康接入 | pending | DF-043 |
| DF-045 | Device Farm Console iOS 设备域页面 | pending | DF-044 |
| DF-046 | iOS 真实验收、运维回滚与 Android 回归 | pending | DF-045 |

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

验收：当前 Host 能创建一台 Android 16/API 36 模拟器；ADB online、boot completed 成功；删除后容器、网络和临时数据清理；无 KVM 时明确失败，不能静默降级冒充通过。多设备端口隔离保留自动化契约测试，资源允许时可执行扩展验收。

### DF-015 Appium Endpoint 和健康 Adapter

实施：为每台 Emulator 建立独立 Endpoint，完成端口分配、状态检查和连接信息返回。

产出：Appium Adapter 和健康探针。

验收：当前 Android 16 设备可创建并删除真实 UiAutomator2 Session；Appium 不健康时设备不得变为 ready；错误 UDID 和多 Endpoint 隔离由自动化测试覆盖，资源允许时可执行多设备扩展验收。

### DF-016 镜像验证和固定目标自动补齐

实施：实现每个 Image 的 `docker_image + docker_digest` 选择、digest 验证和镜像 validation；validation、create、管理员 rebuild 和释放后 rebuild 必须下发同一 Image 引用。当前建立 `max_concurrency=1` 的默认逻辑设备池；使用 PostgreSQL 行锁按 `min_ready/max_instances` 计算缺口，原子登记 provisioning Device、Pool membership 和 Host Command，由 Agent/Docker 自动创建并在 Appium 健康后转为 ready。失败必须退避且可补偿，不实现负载预测或自动删除缩容。

产出：镜像验证任务、固定目标 Controller、Host Command 编排、默认池配置和单设备容量检查。

验收：未验证或缺少运行引用的镜像不能启动设备；两个不同 Image 产生不同 `docker_image` Host Command，Agent 校验摘要后按所选镜像创建，rebuild 保持原 Image；配置 `min_ready=1/max_instances=1` 后自动创建并加入一台 Emulator；删除或隔离后自动补回；两个 Controller 并发运行不超建；Docker/KVM/Appium 持续失败时有退避且不形成命令风暴；第二个并发预约保持 pending/capacity unavailable；提高目标数量只改配置，降低目标不会自动删除在用设备；真机不被自动创建。

## 7. 阶段 E：STF 和真实执行

### DF-017 STF 与 RethinkDB 部署

实施：提供固定版本和配置的内网部署，完成 Emulator 发现、日志和远控基础验证。

产出：Docker Compose、配置样例和健康检查。

验收：当前一台 Emulator 在 STF 显示正确 serial 和 ready 状态；重启 STF 不改变设备农场 reservation 真相；管理 Token 不暴露给浏览器。多设备 inventory 在扩展环境验证。

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

## 9. 阶段 G：独立设备控制后台

### DF-026 Device Farm Console 工程和只读页面

实施：创建 `console` pnpm + Vite + React + TypeScript + Ant Design 工程，使用 Orval + TanStack Query 从 OpenAPI 生成强类型 client；在现有 Go Server 中增加配置用户、`console` Principal、PostgreSQL 可撤销短时会话、CSRF、登录限流和 `/console/` 静态入口；实现设备总览以及 Image、Host、Pool、Device、Reservation、健康事件的列表和详情只读页面。浏览器不能接触 Service Token。

产出：`console/` 源码、构建入口、静态部署配置、浏览器认证/会话说明、组件测试和 Mock API 页面测试。

验收：E0 本地 PostgreSQL + Mock Provider 环境可安装依赖、生成 client、构建并执行组件/Playwright 测试；migration up/down/up 通过；登录、错误密码、限流、注销、过期、CSRF、伪造 actor 和 viewer/operator/admin 只读权限通过；未认证用户不能读取设备数据；刷新页面后状态与 Server 一致；浏览器网络、存储、构建产物和错误信息中不存在 Service/Agent/STF Token；前端没有 Case、Dataset、Run、Result、评分和报告模块。无需 Linux/STF/Appium 即可 completed。

### DF-027 设备操作和人工预约页面

实施：在 DF-026 基础上实现 Image validation、Host drain/undrain、Pool 配置、Device restart/rebuild/quarantine/unquarantine、人工 Reservation 创建/轮询/续租/释放和设备域审计展示。使用 Mock Provider 和 Mock STF 完成 claim/release 编排；危险操作必须二次确认、填写 `reason` 并显示服务端 request ID；页面不得直接调用 STF、Docker、ADB、Appium 或数据库。按 ADR-0010，Console 不展示 `remoteConnect` TCP 地址。

产出：完整设备控制流程页面、状态与权限映射、表单校验、错误/重试体验、端到端浏览器自动化测试和 `docs/evidence/DF-027/` 证据。

验收：E0 本地 Server + PostgreSQL + Mock Provider + Mock STF 中，用户可完成“查看容量 → 创建人工预约 → 等待 active → 续租或释放 → 查看审计”；非法状态操作被页面和 Server 同时拒绝；Console 用户不能修改 owner 或访问他人预约；第二个预约不突破单设备容量；浏览器拿不到 STF 管理 Token，也不展示 ADB TCP 地址；刷新或 Server 重启后不保留虚假成功状态。无需真实 Linux/STF 即可 completed。

### DF-028 控制台部署、安全和真实 Web 验收

实施：把 Console 纳入正式部署、健康检查、升级、回滚和运维手册；配置 HTTPS/受控内网访问、内容安全策略、Cookie/CSRF、防缓存和静态资源版本；在一台 Android 16 Emulator、STF、RethinkDB、Appium 和真实 Device Farm Server 上执行 Web 端到端验收，完成后清理临时资源。

产出：生产构建与部署配置、Web 安全清单、浏览器端到端报告、关键页面截图、回滚演练和 `docs/evidence/DF-028/acceptance.md`。

验收：新环境按文档可部署并访问；未认证、越权、CSRF、过期会话和直接内部端口访问均被拒绝；用户通过浏览器完成资源查看、预约、续租、释放、隔离/恢复或重建验证；按 ADR-0010 确认页面不展示 STF `remoteConnect` TCP 地址且不泄露任何内部 Token；Server/STF/Console 任一重启后状态收敛；回滚成功且无临时容器、网络、卷、会话或凭证残留。

### DF-029 控制台统一容量和自动安全缩容

实施：按 ADR-0011 将 Pool Image 的固定容量收敛为控制台单一“目标设备数”；Server 原子同步 `min_ready/max_instances`、Pool `max_concurrency` 和 Host `device_slots` 高水位；Host 心跳不得用 Agent 命令并发覆盖后台容量；Warm Pool Controller 增加超额计算、最旧空闲设备选择、Pool 退出、delete Host Command、成功标记 deleted、失败隔离和审计。占用设备等待释放，不强制删除。总览区分当前实例与历史 Device/Reservation。

产出：ADR-0011、容量配置 API 语义、自动缩容 Controller、Console 单一目标表单、并发/故障测试和 `docs/evidence/DF-029/`。

验收：当前 ADR-0008 验收主机以两台为安全上限，后台把目标从 1 改为 2 后无需修改或重启 Host Agent 即自动补齐两台，真实 `2 → 1` 删除最旧 Emulator 并保留最新；`3 → 1` 多设备删除语义由真实 PostgreSQL 集成测试覆盖，后续更换高内存服务器时补充容量压力验证。最旧设备 active 时不强删，释放后继续缩容；两个 Controller 并发只为每台超额设备生成一条 delete Command；删除失败三次后设备 quarantined 且不被替代实例掩盖；容器、网络和卷真实清理；Pool 并发与目标一致；历史记录不计入当前运行容量；全部操作有 actor、reason 和 request ID。

### DF-030 隔离设备受控人工删除

实施：按 ADR-0012 为 `DELETE /api/v1/devices/{id}` 增加管理员人工删除编排。仅允许 `quarantined/stopped` 且没有 pending/active Reservation 的 Device；请求必须包含 reason 和 Idempotency-Key。Server 在 PostgreSQL 事务中退出 Pool、登记审计和持久化 delete Host Command，由 Agent/Provider 清理容器、网络、端口和卷。成功后标记 `deleted` 并清空 Endpoint，失败保持或回到 `quarantined/unhealthy`。Console 只在允许状态展示危险删除按钮并进行二次确认。

产出：OpenAPI、Management Service/Store、Host Command 完成收敛、Console 删除入口、PostgreSQL 集成测试和真实 Linux 验收证据。

验收：ready/reserved/busy/recycling Device 删除返回 409；存在活动预约时拒绝；同一幂等键只产生一个 delete Command，同设备已有 pending/leased delete Command 时更换幂等键也拒绝重复创建；成功结果必须包含 `deleted=true`，随后设备转为 deleted、Pool membership 禁用、Endpoint 清空且审计/健康事件完整；失败三次后设备 quarantined/unhealthy；目标数量不变时 Warm Pool 可补建；浏览器、Server 均不访问 Docker Socket。

### DF-031 管理员设备远程控制

实施：按 ADR-0013 在 Device 页面增加管理员“一键远程连接”。Server 为指定 `ready/healthy` Device 创建 `manual` 短租约 Reservation，Scheduler 精确选择该 Device 并完成 STF claim；active 后 Server 为固定 STF 管理员身份签发极短有效的 HS256 Web 登录 JWT，Console 在预先打开的新标签页中进入 STF `/#!/control/{serial}`。Console 发送心跳并检测标签页关闭；挂断、关闭、STF 自行释放或心跳超时都复用 Reservation release/Reaper、STF release 和设备 rebuild 链路。

产出：ADR-0013、远控 Console API/OpenAPI、精确 Device 调度约束、STF Web JWT 签发器、Device 页面连接/打开/挂断交互、部署配置、安全测试、浏览器测试和 `docs/evidence/DF-031/` 真实证据。

验收：只有 admin 和 `ready/healthy` Device 可以开始；点击后无需 STF 账号密码并直接进入指定设备；真实看屏、点击、滑动、输入、Home 和返回有效；第二条远控不双占；JWT 在 STF 重定向后从地址移除且响应/日志/存储无 STF API Token 或签名 Secret；点击挂断和关闭远控标签页均释放 Reservation/STF claim 并触发重建，最终 `ready/healthy`；Console 崩溃或断网后短租约由 Reaper 在限定时间内回收；STF 页面主动释放后后台状态收敛；Server/STF 重启无永久 active 远控。

### DF-032 动态 Host 容量和设备运行规格

实施：按 ADR-0014 定义强类型 Emulator 运行规格；Host Agent 自动上报实际逻辑 CPU、总/可用内存、Docker 数据盘总/可用空间和 GPU 能力；Warm Pool 在 PostgreSQL 行锁内按已有 Device、pending/leased 创建类命令和待创建规格预留资源。Agent 执行创建前再次按实时资源预检。Docker Provider 必须把完整规格用于容器和 Guest，不再把全局 CPU/内存当成每台最终配置。

产出：ADR-0014、OpenAPI 规格与容量模型、资源探测器、Server/Agent 双重预检、Docker 参数映射、单元/集成测试和 `docs/evidence/DF-032/`。

验收：代码没有固定一台/两台上限；同一 Host 对 8 GB 和 4 GB 规格返回不同可新增数量；CPU、内存或磁盘任一不足均不创建 Device/Command 并返回具体缺口；两个 Controller 并发不超分；共享镜像层不按设备数重复扣减，单设备数据盘会重复扣减；心跳陈旧、规格非法或实时资源下降时安全拒绝；现有一台真实 Android 16 仍可创建、进入 STF/Appium 并受 Docker/Guest 规格约束。

### DF-033 Pool 总目标和默认镜像

实施：覆盖 ADR-0011 的按 Image 固定目标语义，为 Pool 增加总目标、最小预热和默认 Image；最大并发独立配置且不得超过总目标。Warm Pool 自动补建只使用默认 Android 16，不把 Android 13～16 的 Image 目标相加。迁移现有单 Image Pool 时保持当前设备和目标，不触发意外删除。

产出：migration、OpenAPI、Management/Controller、Console Pool 表单、迁移/并发/缩容测试和 `docs/evidence/DF-033/`。

验收：测试环境目标 1 正常；资源足够时目标可改为 2、3 或更高且无需修改 Host Agent；资源不足时保留目标并显示待扩容和明确原因，不伪造 Host 容量；默认 Image 切换不立即重装现有设备，后续自动补建使用新默认；缩容继续只删除最旧空闲设备。

### DF-034 设备规格编辑和受控重装

实施：Device 保存运行规格覆盖和待应用配置。Console 为无活动预约、无在途命令的 `ready/stopped/quarantined` Emulator 提供“编辑配置/更换镜像”；Server 校验动态容量并创建可恢复的 reimage Host Command。Agent 清理旧容器/卷后按目标创建并验证 ADB、STF、Appium；成功后切换当前 Image/规格，失败尝试恢复旧配置一次，最终失败隔离。

产出：migration、OpenAPI、重装编排、Agent/Provider 支持、Console 表单与二次确认、故障恢复测试和 `docs/evidence/DF-034/`。

验收：使用中的设备不能编辑；4 GB 改 8 GB 时按实际剩余容量判断；重装明确提示会清空 APK 和设备数据；成功后 Device ID/Pool membership 不变，Image、有效规格和动态 Endpoint 正确更新；失败不把数据库伪装成目标 Image，恢复失败时隔离且审计可追踪；刷新页面不会丢失处理中状态。

### DF-035 官方 Android System Image 目录同步、后台按需准备和真实多规格验收

实施：Server 同步 Android SDK 官方稳定频道的 System Image 目录；Console 仅能选择目录中的 Android/API、`google_apis`/`google_play` 和 ABI，并继续配置 runtime profile。点击“准备镜像”后，Server 创建可恢复的异步构建命令，由受控 Build Agent 用固定版本 `sdkmanager`/`avdmanager` 下载、叠加既有 Appium、UiAutomator2 和启动脚本，推送内部 Registry 并记录不可变 digest。只有既有验证成功后才写入 `device_images` 为可用；已缓存 digest 直接复用。Android 16 为默认候选，不预先创建四台设备。按 Host 能力验证 GPU host/auto/software 回退以及两种内存规格的容量结果。

产出：官方目录同步契约、受控构建 Agent 命令、镜像准备状态/审计、不可变 digest/缓存策略、容量与重装真实验收、回滚说明及 `docs/evidence/DF-035/`。

验收：Console 可显示可下载、下载/构建中、验证中、已缓存可使用、准备失败和官方已更新；浏览器不访问 Google 且不能提交任意下载地址/命令；Build Agent 使用官方稳定目录的固定包名并记录 digest；验证成功前 `device_images` 无候选记录；相同 digest 复用缓存；默认创建 Android 16；管理员可把同一空闲 Device 重装到已可用版本并通过 ADB、STF、Appium 冒烟；镜像层缓存与设备卷占用在 Console 中可区分；测试 Host 最终只保留用户设定的设备数量和默认版本，不因目录条目自动创建四台。

### DF-036 旧镜像受控停用和可用镜像选择

实施：为 Device Image 增加带原因、并发保护和设备域审计的受控停用；仍被活动 Device 引用或作为任一 Pool 默认值时拒绝，只有历史 `deleted` Device 引用时允许停用并禁用旧 Pool Image 关系。Console 默认只显示已验证可用镜像，可切换查看已停用归档；管理员可在 Image 页面将任意 `ready` 镜像选择为指定 Pool 的默认镜像，自动启用该 Pool Image 关系，但不重装已有 Device。

产出：ADR-0016、OpenAPI、Management Service/Store、Console 镜像选择与归档视图、PostgreSQL 集成测试和 `docs/evidence/DF-036/`。

验收：旧镜像仍是 Pool 默认值或被非 `deleted` Device 引用时停用返回 409；只被历史 Device 引用时停用成功、Pool Image 关系禁用、审计完整且默认列表不再显示；归档视图仍可追溯。选择已验证 Image 后其 Pool Image 关系启用且成为默认值，后续补建使用它，已有 Device 不发生重装；非 `ready` Image 不可选择。真实环境清理旧 DF-035 前镜像后只显示当前可用镜像，当前 ready/healthy 设备、STF 和 Appium 不受影响。

### DF-037 Phone 硬件模板和受控模拟器创建向导

实施：只为 Phone 提供 Android SDK 硬件模板搜索和选择；从已同步的官方 System Image 目录选择系统版本，未准备条目明确显示准备状态，已验证条目可直接创建。创建页提供容器/Android CPU 和内存、数据盘、分辨率、DPI、VM Heap、图形模式等完整 runtime profile，并选择目标 Pool。Server 必须在一个数据库事务内锁定 Pool 和 Host 实际容量、登记 provisioning Device、Pool membership 与 `create` Host Command、增加 Pool `total_target`；Agent/Provider 使用指定 Phone Profile 创建，既有 Controller 完成 ADB、STF、Appium 健康收敛。首期不提供 Tablet、Wear、TV、Automotive、Desktop 或 XR。

产出：ADR-0017、Phone Profile 目录接口、受控创建 API、Provider Profile 参数映射、Console 创建向导、单元/集成/契约测试和 `docs/evidence/DF-037/`。

验收：Console 可搜索并从多条 Phone 模板选择；系统镜像目录不再只允许 API 33～36 四项，未准备项不会被误认为可创建；提交后不会由浏览器或 Server 直连 Docker/Google，设备、Pool membership、Host Command 和目标数原子登记；容量不足或非 ready 镜像返回稳定错误且无半条记录；指定 Profile、CPU、内存、分辨率和 GPU 参数进入 Agent/Provider；真实 Linux KVM 创建后通过 ADB、STF、Appium 才进入 ready/healthy。

### DF-038 长期设备、基础设备扩容和 Android Studio 式创建流程

实施：以 Device 页面为唯一日常入口，新增四步创建向导：选择 Phone、Android SDK 版本、Pool 和高级 runtime profile 后创建；向导提交 `catalog_id` 至持久化 provisioning job，未缓存版本由受控 Server/Agent 链路自动准备并在验证后继续创建，刷新和幂等重试恢复同一 job，镜像缓存移出主导航。每个 Pool 可选择一台 ready/healthy Phone Emulator 作为基础设备，后续扩容只复制其已生效 Image、Phone Profile 和 runtime profile，数据卷保持干净。Reservation release 只释放占用并把 Device 从 busy 返回 ready，不再自动 rebuild 或删除数据卷。管理员可删除没有活动预约的 ready/quarantined/stopped Device；删除同时降低所属 Pool 的总目标，避免自动补建；删除基础设备必须先选定替代基础设备。

产出：ADR-0018、Pool 基础设备持久化和迁移、创建准备编排、release 保留数据、直接删除编排、OpenAPI/Console 与 `docs/evidence/DF-038/`。

验收：设备使用后 APK、账号、缓存和文件保持；显式 rebuild/reimage 仍恢复出厂。创建 job 覆盖已缓存直接创建、未缓存自动准备、失败阶段、页面刷新恢复和同一幂等键只生成一个 job/Device/Host Command/Pool 目标。基础设备修改成功后新扩容实例使用其最新 Phone/Image/runtime profile，但不复制其数据；非本 Pool、非 healthy Phone 或删除前未切换替代基础设备均拒绝。ready 空闲设备可直接删除且 Pool 目标同步减少；reserved/busy/recycling 或有活动预约设备拒绝删除；真实 Linux KVM 验证创建、release 保留数据、基础设备扩容以及删除不自动补回。

## 10. 阶段 H：新版 Alcor 接入

### ALCOR-001 新版 Alcor 真实接口联调与统一入口

本地新版 Alcor `test` 分支已具备 Android 接入实现，本任务进入真实联调；正式分支合并和正式部署不属于本地完成条件。

开工门禁已由本地新版 Alcor `test` 分支满足：RunAttempt/Worker、Android Executor、Device Farm 配置、受控操作者 Header、Artifact 和真实 DaFit 链路均已出现并完成阶段性验收，审计见 `docs/evidence/ALCOR-001/readiness.md`。

实施：核对认证、RunAttempt、取消、ArtifactStore 和关联 Header；实现 Worker Device Farm Adapter；在 Alcor 一级导航以同源受控代理嵌入既有 Device Farm Console，钉钉登录一次即可管理设备、预约和远控，APK 版本作为同页二级区域继续由 Alcor 管理；执行双方契约和真实 DaFit/Android 冒烟。

验收：Eval Console 创建 Run 后，Worker 自动申请设备、执行、写 ClickHouse/Supabase、释放设备；RunAttempt 与 Device Session 可双向追溯；失败正确映射为 failed 或 infra_failed；Alcor 设备入口与独立 Device Farm Console 不产生两套设备状态或操作语义；浏览器没有 Service/STF Token，设备操作审计记录钉钉操作者，打开设备入口无需第二次登录。

## 11. 第二版多平台设备农场

### DF-039 第二版多平台宿主机与 iOS 接入设计

实施：只做设计、复用验证和真实环境盘点，不修改 API、数据库、状态机或 Provider 代码。固定 Android 第一版归档基线和本地第二版分支；核对 Appium Device Farm、Appium 3、XCUITest、WebDriverAgent、go-ios、macOS/Xcode 和 iOS 真机/Simulator 的版本与职责；明确现有 PostgreSQL Scheduler、Pool、Reservation、Reaper 和审计继续作为唯一设备占用真相，Appium Device Farm 只能作为宿主机侧发现、连接和 Session 路由候选组件；设计按明确 UDID 使用设备的防双分配约束、平台中立健康模型、连接快照、Android STF 保留策略和 iOS 人工远控边界。

产出：第二版专项 ADR、更新后的复用矩阵、架构对齐表、功能方案、分步实施任务、验收环境和 `docs/evidence/DF-039/` 证据。后续实现任务只有在这些文档明确允许后才能新增。

验收：可以明确回答 macOS Host、iOS 真机和 Simulator 分别如何发现、签名、健康检查、预约、建立 XCUITest Session、释放和故障收敛；证明不会让 Appium Device Farm 与 PostgreSQL 各自独立分配同一设备；明确 Appium Device Farm 12.x 不提供当前版本的人工串流，因此不把它描述为 STF 的跨平台远控替代；Alcor/DaFit/STF/Appium 的既有职责没有被复制；没有写入任何生产代码、migration 或真实凭证。

### DF-040 平台中立设备域模型与契约

实施：按 ADR-0021 为 Host、Pool、Device、Connection 和健康模型增加明确平台语义；migration 将现有数据回填为 Android，Pool 禁止混合平台，Device 支持 `simulator/physical` 和 iOS Provider；允许同 Host 多台 iOS Device 共享 Appium Endpoint，同时保持 UDID/serial、Provider identity 和 active Reservation 唯一。更新 OpenAPI、领域状态机、Scheduler 能力匹配、Mock 与契约测试，不接入真实 Appium Device Farm。

产出：可回滚 migration、平台中立领域模型、OpenAPI、Repository/Scheduler 适配、Mock 测试和证据。

验收：migration up/down/up；Android 旧数据和全部第一版测试无回归；iOS Pool 不能加入 Android Device；共享 Endpoint 合法但重复 UDID 被拒绝；100 个并发请求竞争一台 Mock iOS Device 仍只有一个 active Reservation。

### DF-041 macOS Host Agent 与 Appium Device Farm Adapter

实施：让现有 Host Agent 在 macOS 运行并上报 host_os、架构、Xcode/Runtime、Node、Appium、Device Farm、XCUITest、WDA/go-ios 版本与脱敏 readiness；新增固定 12.0.1 的 Appium Device Farm Adapter，只读取本机 inventory、busy 和 Node 健康。首期每台 Host 独立 Node，不启用跨 Host Hub 分配，不把插件数据库同步为业务表。

产出：macOS 构建/部署入口、版本锁、Adapter 契约、Host 心跳扩展、故障分类和 E4 环境部署说明。

验收：版本不匹配、Xcode license、Appium doctor、Node 离线均阻止新预约；未知设备只登记 unknown/quarantined；插件凭证和 Apple Secret 不进入心跳、日志或 API；Android Linux Agent 回归通过。

### DF-042 Reservation 绑定的 iOS Session Fence

实施：新增基础设施级 Session Fence。它只接受短时单次 Session Grant，校验 active Reservation、Device、Host、Endpoint 和 UDID，强制相同的 `appium:udid` 与单元素 `df:udids` 后透明转发 Appium Session；保存 Appium Session ID 技术绑定并让 Reaper 关闭遗留 Session。禁止 tags、filterByHost、多 UDID 和浏览器直连 Node，不解释或实现 WebDriver 业务命令。

产出：Session Grant/Fence 契约、技术绑定持久化、网络配置、漂移 Reconciler、故障测试和审计。

验收：无 Reservation、错误/多 UDID、重放 Grant、跨 Host Endpoint 均拒绝；插件 busy 与 Reservation 不一致时停止分配并隔离；Session 删除/过期后 busy、Device Session 和 Reservation 收敛；任何时刻同一 Device 最多一个 active Session。

### DF-043 iOS Simulator 固定库存接入

实施：在 E4 只接入管理员 allowlist 中已经创建并 booted 的 Simulator；Agent 报告 UDID、Runtime、机型和健康，Server 通过既有 Pool/Reservation 管理固定库存。首期不下载 Runtime、不自动克隆或删除 Simulator，启动/停止操作必须使用受控 Host Command。

产出：Simulator inventory/health Provider、受控命令、Pool/Reservation 集成、E4 部署和证据。

验收：两台不同 UDID Simulator 可发现、加入单平台 Pool、分别预约并并发建立 XCUITest Session；shutdown、boot timeout、Agent 重启和 UDID 冲突正确收敛；50 次循环无永久 busy、双占或端口/Session 泄漏。

### DF-044 iOS 真机、WDA 签名与健康接入

实施：接入固定 allowlist 真机，检查配对/信任、Developer Mode、UI Automation、iOS/Xcode 兼容和 WDA 签名 readiness；签名只使用 macOS Keychain/Secret 引用，服务端保存非敏感 Team/bundle/到期摘要。复用 XCUITest/WDA/go-ios，不管理 IPA，不启用非 macOS iOS 模式。

产出：真机 inventory/health、签名 readiness、WDA 预装/启动策略、故障分类、轮换和恢复手册、E5 证据。

验收：至少一台实际 iPhone 以明确 UDID 完成 Session、最小 XCUITest 操作和释放；未信任、Developer Mode 关闭、签名过期、WDA 启动失败和版本不兼容都不可调度且原因明确；20 次循环无永久 busy/Reservation，证据无 Apple Secret。

### DF-045 Device Farm Console iOS 设备域页面

实施：在现有 Host、Pool、Device、Reservation 和审计页面增加平台筛选、iOS 机型/版本、真机/Simulator、组件健康和签名到期摘要；操作继续调用设备 API。iOS 页面明确人工远控不支持，不显示 Android STF 操作、Appium Endpoint、Dashboard、WDA 地址或 Session Grant。

产出：Console 页面、OpenAPI client、权限/安全测试和真实浏览器证据。

验收：viewer/operator/admin 权限正确；跨平台操作受服务端校验；浏览器构建、网络和存储无内部 Endpoint/Secret；刷新后与 Server 一致；Android Console 和 STF 原生远控无回归。

### DF-046 iOS 真实验收、运维回滚与 Android 回归

实施：按 `docs/09_ios_device_farm_v2_acceptance.md` 在 E4/E5/E6 执行全量 P0/P1，完成多 Host、过期回收、漂移、稳定性、指标、告警、备份、升级、排空和版本回滚；同时执行 Android 第一版真实链路和 Alcor Device Farm Adapter 契约回归。iOS 业务 Executor 的开发仍由 Alcor 另立任务。

产出：完整脱敏证据、运维/故障/回滚文档、版本清单、已知问题和最终签收记录。

验收：iOS Simulator 50 次、真机至少 20 次循环无双占/串机/永久 busy；Host/Agent/Appium 故障 120 秒内收敛或隔离；回滚后 Android 继续可用；没有把 Mock/Simulator 结果冒充真机通过。

## 12. 单任务完成定义

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
