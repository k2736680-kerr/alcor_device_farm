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
| DF-039 | 第二版多平台宿主机与 iOS 接入设计 | completed | DF-038、ALCOR-001、Android 第一版归档基线 |
| DF-040 | 平台中立设备域模型与契约 | completed | DF-039 |
| DF-041 | macOS Host Agent 与 Appium Device Farm Adapter | completed | DF-040 |
| DF-042 | Reservation 绑定的 iOS Session Fence | completed | DF-041 |
| DF-043 | iOS Simulator 固定库存接入 | completed | DF-042 |
| DF-044 | iOS Simulator 动态创建、重建和删除 | completed | DF-043 |
| DF-045 | Device Farm Console iOS 设备域页面 | completed | DF-044 |
| DF-046 | iOS Simulator 受控远程控制 | completed | DF-045、ADR-0025 |
| DF-047 | iOS 真实验收、运维回滚与 Android 回归 | completed | DF-046 |
| DF-048 | 统一 Android 与 iOS 设备池自动伸缩 | completed | DF-047 |
| DF-049 | 统一设备农场控制台功能与中文文案审校 | completed | DF-048 |
| DF-050 | 使用 Baguette 替换并清理自写 iOS 远控 | completed | DF-049、ADR-0026 |
| DF-051 | 修复普通成员远控入口与独立网关可达性 | completed | DF-050 |
| DF-052 | 修复多设备并存时远控安装目标错配 | completed | DF-051、ADR-0027 |
| DF-053 | 受管虚拟设备自动淘汰替换与可用性收敛 | completed | DF-048、DF-052、ADR-0028 |
| DF-054 | 设备农场控制台全页面可用性审校 | completed | DF-049、DF-053 |
| DF-055 | 简化设备运行视图并清理历史噪声入口 | completed | DF-054 |
| DF-056 | 正式数据清理与长期设备非破坏自愈 | completed | DF-055、ADR-0029 |
| DF-057 | 全仓不可达代码与旧策略清理 | completed | DF-056 |
| DF-058 | 独立控制台安全保持登录 | completed | DF-057 |
| DF-059 | 修复正式控制台密码哈希并完成真实登录验收 | completed | DF-058 |
| DF-060 | 设备可读名称与指定设备排队 | completed | DF-059 |
| DF-061 | 修复跨入口挂断与过期预约回收 | completed | DF-060 |
| DF-062 | 修复 Emulator OOM 后的幽灵占用与原机恢复 | completed | DF-061、ADR-0029 |

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

实施：实现 extension、主动 release、强制 release、grace period 和数据库锁保护的 Reaper。按 ADR-0023，最大租期表示相对数据库当前时间的有限滑动窗口；运行方周期续约时不得因累计运行超过一小时而强制结束。

产出：租约 service、后台 Reaper、时间可控测试。

验收：合法续租更新 expires_at；单次续约和任意时刻的未来到期时间不超过设备池最大窗口；累计运行超过四小时仍可续约；过期预约不能复活并在目标时间内关闭；并发 release 幂等；两台 Reaper 同时运行只回收一次。

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

实施：让现有 Host Agent 在 macOS 运行并上报 host_os、架构、Xcode/Runtime、Node、Appium、Device Farm、XCUITest、WDA/go-ios 版本与脱敏 readiness；新增固定 12.0.1 的 Appium Device Farm Adapter，只读取本机 inventory、busy 和 Node 健康。每台 Host 使用本机 Hub/Node，不启用跨 Host Hub 分配，不把插件数据库同步为业务表。

产出：macOS 构建/部署入口、版本锁、Adapter 契约、Host 心跳扩展、故障分类和 E4 环境部署说明。

验收：版本不匹配、Xcode license、Appium doctor、Node 离线均阻止新预约；未知设备只登记 unknown/quarantined；插件凭证和 Apple Secret 不进入心跳、日志或 API；Android Linux Agent 回归通过。

### DF-042 Reservation 绑定的 iOS Session Fence

实施：新增基础设施级 Session Fence。它只接受短时单次 Session Grant，校验 active Reservation、Device、Host、Endpoint 和 UDID，强制相同的 `appium:udid` 与单值字符串 `df:udids=<reserved_udid>` 后透明转发 Appium Session；保存 Appium Session ID 技术绑定并让 Reaper 关闭遗留 Session。`providerBusy` 作为占用事实继续上报，不把正常活动 Session 误判为 Router 故障；正常清理后留 30 秒等待心跳刷新 busy。禁止 tags、filterByHost、多 UDID 和浏览器直连 Node，不解释或实现 WebDriver 业务命令。

产出：Session Grant/Fence 契约、技术绑定持久化、网络配置、漂移 Reconciler、故障测试和审计。

验收：无 Reservation、错误/多 UDID、重放 Grant、跨 Host Endpoint 均拒绝；插件 busy 与 Reservation 不一致时停止分配并隔离；活动 Session 期间 Host 保持 online、Device 保持 busy/healthy；Session 删除/过期后 busy、Device Session 和 Reservation 收敛且不会在清理心跳宽限内误隔离；任何时刻同一 Device 最多一个 active Session。

### DF-043 iOS Simulator 固定库存接入

实施：在 E4 只接入管理员 allowlist 中已经创建并 booted 的 Simulator；Agent 报告 UDID、Runtime、机型和健康，Server 通过既有 Pool/Reservation 管理固定库存。首期不下载 Runtime、不自动克隆或删除 Simulator，启动/停止操作必须使用受控 Host Command。

产出：Simulator inventory/health Provider、受控命令、Pool/Reservation 集成、E4 部署和证据。

验收：两台不同 UDID Simulator 可发现、加入单平台 Pool、分别预约并并发建立 XCUITest Session；WDA 冷启动在 300 秒安全窗口内不得被漂移检查误隔离；shutdown、boot timeout、Agent 重启和 UDID 冲突正确收敛；50 次循环无永久 busy、双占或端口/Session 泄漏。

### DF-044 iOS Simulator 动态创建、重建和删除

实施：按 ADR-0024 让 macOS Agent 上报已安装且受控的 iOS Runtime 与 iPhone Device Type 目录；新增受控创建 API，复用现有 Device、Pool、Host Command、容量预检、幂等、审计和 Provider 链路执行 `simctl create/boot/bootstatus`。为无活动 Reservation/Session 的 Simulator 实现 shutdown、erase 重建和 delete；调用方不能提交 shell 或任意参数。

产出：CoreSimulator 目录、Provider 动态生命周期、Server 创建编排、OpenAPI、容量/并发/故障测试、macOS 部署说明和 E4 证据。

验收：从后台分别选择 Host 已安装的 Runtime 和受控 iPhone 类型创建 Simulator，最终进入 ready/healthy 并可按明确 UDID 建立 XCUITest Session、读取最小 `/source` 后删除 Session；stop/start、erase rebuild、delete 均真实通过；容量不足返回中文缺口且无半条 Device/Command；busy 或有活动 Reservation 时拒绝 rebuild/delete；Agent 重启后动态设备可重新发现；重复幂等请求不重复创建；删除后无残留 UDID、Session、Reservation 或 provider busy。

### DF-045 Device Farm Console iOS 设备域页面

实施：在现有 Host、Pool、Device、Reservation 和审计页面增加平台筛选、iOS Runtime/机型、Simulator、组件健康以及动态创建向导；操作继续调用设备 API。iOS 页面明确人工远控不支持，不显示 Android STF 操作、Appium Endpoint、Dashboard、WDA 地址或 Session Grant。

产出：Console 页面、OpenAPI client、权限/安全测试和真实浏览器证据。

验收：viewer/operator/admin 权限正确；跨平台操作受服务端校验；浏览器构建、网络和存储无内部 Endpoint/Secret；刷新后与 Server 一致；Android Console 和 STF 原生远控无回归。

### DF-046 iOS Simulator 受控远程控制

实施：复用 Appium Device Farm 的明确 UDID 路由、Appium XCUITest Session、WDA MJPEG/动作，以及既有 `remotecontrol.Service` 的 Reservation、滑动租约、心跳、释放、Reaper 与审计。Server 通过 Session Fence 为当前管理员独占预约创建人工 Session，只向浏览器签发同源短时入口和固定画面/动作白名单，不返回 Host、Fence、Appium、WDA、MJPEG、Agent Token 或 Session Grant。Android 继续使用 STF，不复制或迁移 STF 协议。

产出：远控传输抽象、Session Fence 受控画面/动作接口、同源 iOS 远控页、Console 入口、部署/回滚说明、权限/双占/泄露测试和真实 Mac 证据。

验收：管理员只能看到和操作已预约的目标 iOS Simulator，画面不含 macOS 桌面；截图/MJPEG、点击、滑动、文本和 Home 有效；远控与自动化 Session、其他操作者、drain、隔离和超时均互斥且正确收敛；浏览器无法直连 Fence/Appium/WDA/MJPEG 或发送任意 WebDriver 命令；Android STF 远控无回归。

### DF-047 iOS 真实验收、运维回滚与 Android 回归

实施：按 `docs/09_ios_device_farm_v2_acceptance.md` 在 E4/E6 执行当前动态 Simulator P0/P1，完成多设备、过期回收、漂移、稳定性、指标、告警、备份、升级、排空和版本回滚；同时执行 Android 第一版真实链路和 Alcor Device Farm Adapter 契约回归。真实 iPhone 与 iOS 业务 Executor 另立后续任务。

产出：完整脱敏证据、运维/故障/回滚文档、版本清单、已知问题和最终签收记录。

验收：iOS Simulator 创建/Session/重建/删除至少 50 次循环无双占、串机、永久 busy 或 CoreSimulator 残留；Host/Agent/Appium 故障 120 秒内收敛或隔离；回滚后 Android 继续可用；报告明确当前未宣称真实 iPhone 已接入。

### DF-048 统一 Android 与 iOS 设备池自动伸缩

实施：复用现有 Pool `total_target/min_ready/max_concurrency`、基础设备字段、Host Command、Host Agent、容量预检和 Provider 生命周期，为 iOS Pool 增加与 Android 一致的固定目标自动伸缩。管理员先从本 Pool 选择一台 `ready/healthy` iOS Simulator 作为扩容模板；扩容只复用其 Mac Host、Runtime 与 iPhone Device Type 创建全新 CoreSimulator，不复制设备数据；缩容只删除没有活动 Reservation/Session 和在途命令的最旧空闲 Simulator。Console 的目标输入不再按平台禁用，实际数量、目标、差额、模板缺失和容量不足必须使用真实 API 数据与中文说明。

产出：iOS Pool 目标 Controller、模板设备校验、Android/iOS 通用缩容、Console 目标表单、并发/容量/安全缩容测试和 `docs/evidence/DF-048/`。

验收：iOS 目标从 3 调到 6 时，在模板和容量满足的情况下只创建 3 台且每台 Runtime/机型与模板一致、数据全新；两个 Controller 并发不超建；模板缺失、Host 排空/离线、内存/磁盘/槽位不足时保留真实目标并显示中文原因，不留下半条资源；目标降低时不强删使用中设备，空闲设备经既有 Agent/CoreSimulator 删除链路收敛；Android 自动扩缩容和 iOS 手工创建/删除均无回归。

### DF-049 统一设备农场控制台功能与中文文案审校

实施：逐页审核导航、总览、宿主机、设备池、设备、预约、健康事件、审计、登录和远控中的字段名、按钮、确认、成功/失败提示、空状态和帮助文字。产品口径统一为 Android+iOS 设备农场；平台名称统一使用 Android/iOS，Android 专属镜像、ADB、STF 与 iOS 专属 CoreSimulator、Runtime、WDA 只在对应平台出现；面向用户的 request ID、错误、租期和状态使用中文名称，不显示失实的迁移、自动清理或单平台说明。同步补齐关键交互测试。

产出：控制台平台化文案清单、页面与标签修正、组件测试和 `docs/evidence/DF-049/`。

验收：全控制台搜索不存在把统一平台误写成纯 Android、把 Mac 当普通模拟器服务器、把释放误写为清理数据或把目标数量写死的文案；Android/iOS 条件字段和操作正确；所有用户提示为中文且包含可追踪请求编号；前端测试和生产构建通过，真实同一后台能同时查看并操作 Android 与 iOS。

### DF-050 使用 Baguette 替换并清理自写 iOS 远控

实施：按 ADR-0026 固定并部署 Baguette，新增只负责健康、目标 UDID 和独立签名 Gateway 的 Adapter；现有开始、查询、心跳和结束 API 继续复用 PostgreSQL Reservation/Lease/Reaper。删除自写 iOS HTML/CSS/JavaScript、WDA MJPEG/截图/动作代理、人工 Appium Session 创建、Fence 远控路由及 Alcor 旧同源代理。Android 继续挂载 STF 原生页面，iOS 挂载 Baguette 原生页面。

产出：ADR-0026、Baguette Adapter/Gateway、Mac 后台服务与安全通道配置、OpenAPI/Console/Alcor 入口修正、旧代码零残留扫描、真实浏览器证据和 `docs/evidence/DF-050/`。

验收：真实 Mac 与浏览器连续操作舒适可用，点击、滑动、文字、Home、应用切换和重连生效；其他 UDID、设备墙、生命周期和插件命令被拒绝；结束或超时后页面失效且 Reservation 回收；Android STF、iOS 自动化 Session Fence、Appium Device Farm inventory 均无回归；仓库和部署中不存在旧自写页面、MJPEG/动作转发或可恢复旧方案的配置；全部提示为中文。

### DF-051 修复普通成员远控入口与独立网关可达性

实施：修正 Console 角色与既有设备 API 权限不一致的问题，使 operator/admin 可使用 Android STF 和 iOS Baguette 人工远控，viewer 保持只读；iOS Gateway 使用与 Android STF 一致的 30.171 可访问主机名，不要求修改 Alcor 代码或新增 Alcor 远控路由。

产出：Console/Server 权限修正、角色回归测试、30.171 部署配置和 `docs/evidence/DF-051/`。

验收：普通 Alcor 设备农场成员可以看到远程连接入口并使用自己预约的 Android/iOS 设备；viewer 不能启动远控；管理员危险操作权限不下放；Baguette HTTP/WebSocket、预约隔离、心跳、挂断和 Android STF 无回归；正式 Alcor 代码与服务不改动。

### DF-052 修复多设备并存时远控安装目标错配

实施：按 ADR-0027 保留 STF/Baguette 原生 APK/IPA 安装实现，不新增 App Build、Artifact 或第二套安装器。Android 的 claim、remoteConnect、release 和 STF Web 单设备入口统一使用预约 Device 的明确 `stf_serial`；iOS Baguette Gateway 将会话 Cookie 按 UDID 隔离，从请求设备路径或同源页面来源选择对应会话，多个设备会话同时存在时禁止模糊回退。只有当前预约 UDID 的 `/simulators/:udid/files` 可透传，错误会话、其他 UDID 或无明确目标均失败关闭。

产出：ADR-0027、Android STF 序列号统一、iOS 多标签页会话隔离、双设备上传目标测试、真实测试环境验证和 `docs/evidence/DF-052/`。

验收：同时打开两台 iOS Simulator 时，分别拖入 Simulator App 包只会向各自 UDID 的 Baguette `/files` 路径发起安装，错误会话不能安装到任一设备；Android STF 入口、claim、远程连接和释放使用同一个 `stf_serial`，不会回退第一台 ADB 设备；上游返回失败时页面不得提示成功；预约结束后安装入口立即失效；正式 Alcor 与正式环境均不改动。

### DF-053 受管虚拟设备自动淘汰替换与可用性收敛

实施：按 ADR-0028 修正 iOS Pool 的容量统计，隔离、停止或不健康 Simulator 不再满足固定目标；Agent 对成功/失败的完整 inventory 明确打标，Server 对完整清单中持续消失的已登记 Simulator 收敛为故障。无活动 Reservation、技术 Session 或在途命令的故障 Simulator 自动复用既有 delete Host Command 清理 CoreSimulator，成功后保留历史记录并按 Pool 原目标创建全新设备；删除失败保留隔离并阻止盲目超建。扩容模板允许在原基础设备淘汰后继续使用已验证的静态 Host/Runtime/Device Type 配置，但每次创建仍重新校验 Host 目录、心跳和容量。Console 以可用、使用中、恢复中、故障表达日常状态，并显示 Pool 目标、已登记、可用、恢复中、故障和缺口；Prometheus 增加平台与 Pool 容量指标和缺口告警。

产出：ADR-0028、inventory 缺失收敛、iOS 自动删除补建 Controller、Pool 容量修正、简化状态展示、平台/Pool 指标与告警、回归测试和 `docs/evidence/DF-053/`。

验收：`total_target=min_ready=2` 且一台 Simulator 进入隔离或从一次成功的完整 inventory 持续消失时，不再显示目标已满足；无占用故障设备只产生一个幂等 delete Command，CoreSimulator 删除成功后自动补建至两台可服务设备且目标不变。删除失败时不创建第三台、不形成命令或事件风暴，并显示明确故障。inventory 请求失败不误删全部设备；基础模板被替换后仍可按其已验证 Runtime/机型补建。Host 页面不把 Agent 在线表述为设备可用，Pool/Device 页面只突出简化可用性。Android、真机、Reservation、Session Fence、STF/Baguette 和 DaFit 回归通过。

### DF-054 设备农场控制台全页面可用性审校

实施：在不新增评估业务对象、不复制 STF/Appium/DaFit 能力且不改变设备域状态机的前提下，逐页审校运行概览、Host、Pool、Device、Reservation、健康事件和操作审计。统一页面标题、用途说明、刷新时间、手动刷新、加载失败和空状态；主表只保留日常判断与直接操作字段，把内部编号、Endpoint、原始 payload、完整资源规格和创建信息移入详情。Pool 直接展示目标、可用、使用中、恢复中、故障和缺口；Reservation 默认突出进行中记录并展示剩余时间与失败结果；响应式和嵌入模式保留清晰页面上下文。

产出：Console 共用页面组件、全页面字段与交互优化、响应式样式、组件测试、浏览器验收和 `docs/evidence/DF-054/`。

验收：每个页面都能明确说明用途、显示最近更新时间、手动刷新并在请求失败时给出可重试错误；主表不直接暴露内部 Host 地址和自动化 Endpoint，技术字段可在详情中按权限查看；Pool 容量、Device 可用性、Reservation 剩余时间和故障原因无需横向查找；当前记录与历史记录有清晰入口；桌面、窄屏和 Alcor 嵌入模式可完成主要查看与操作。前端测试、生产构建、Go 全量测试、静态检查和真实浏览器验收通过。

### DF-055 简化设备运行视图并清理历史噪声入口

实施：控制台日常视图只保留当前可用、使用中和故障设备，移除已删除历史、全部记录、健康事件菜单和逐设备健康记录入口；旧健康事件路由统一跳转到故障设备。宿主机主表以 Agent 与调度状态表达当前可用性，精确心跳时间只保留在详情。设备池允许把目标安全降到零，向 API 保留合法的最小并发值，以支持清空故障模板后重新创建。

产出：精简后的导航、设备和宿主机主表，设备池清空回归测试，现场 Android 故障资源重建与 Android/iOS 真实 Appium 验收证据。

验收：日常页面不再显示 deleted 数量和数千条底层健康事件，旧链接可安全回到故障设备；后台心跳、健康收敛和审计能力继续工作；设备池可从一台安全降到零并恢复；Android 与两台 iOS 均逐台完成真实 Appium Session、页面树读取和会话释放，无残留预约或会话。

### DF-056 正式数据清理与长期设备非破坏自愈

实施：按 ADR-0029 取代 DF-053 的故障虚拟设备自动删除补建。Warm Pool 把所有未显式删除的虚拟设备计入登记容量，iOS 故障设备不再排队自动 delete；Reconciler 对系统产生的 Android Agent/STF 隔离重探原 Device，恢复时回到原预约状态或 ready，持续失败且空闲时只排队一次既有 restart Host Command。管理端允许隔离设备执行 restart，但 rebuild/reimage/delete 继续明确标记为破坏性人工操作。正式启用前先备份 PostgreSQL，在维护窗口清理测试 Reservation、Session、健康事件、审计、Host Command、provisioning job、幂等和 deleted Device 关系，保留当前 Host、Pool、Image、三台 Device 及 Provider 数据。

产出：ADR-0029、非破坏恢复实现、容量与自动删除回归、清理前备份和清理清单、三台设备身份/数据保持证据、`docs/evidence/DF-056/`。

验收：Android 因 Agent/STF 短暂失败进入系统隔离后，真实健康恢复必须使用原 Device ID 自动回到 ready；持续失败最多自动 restart 一次且 Device ID、Provider ref、Pool membership 和数据卷不变。restart 失败只隔离；人工隔离不自动解除；Android/iOS 故障设备均不得产生自动 delete/rebuild/reimage 或替代 create。清理后历史业务/健康/审计/命令/已删除设备计数为零，当前三台设备、两台 Host、两类 Pool/Image 配置存在且逐台 Appium `/source` 验证通过。

### DF-057 全仓不可达代码与旧策略清理

实施：从 Server、Host Agent、Harness、Adapter Mock、Console、Docker 构建和部署脚本入口建立引用清单；使用 Go `deadcode -test`、Staticcheck、`go vet`、TypeScript `noUnusedLocals/noUnusedParameters`、Knip 文件扫描和全文引用核对删除不可达实现。重点移除 ADR-0029 已取代且正式历史已清零的 iOS 自动删除补建完成分支与 Console 旧文案、未调用领域构造器/方法、未调用 Repository 查询、未使用 import 和无效控制流。生成的 OpenAPI Client、独立运维脚本、E2E fixture、迁移、回滚和历史证据不因单纯“没有 import”而删除。

产出：删除清单与保留理由、持续静态检查配置、全量回归和 `docs/evidence/DF-057/`。不新增兼容实现、第二套 Adapter、业务对象、API、表或状态。

验收：`deadcode -test ./...` 零结果；Staticcheck 排除纯样式 `ST*` 规则后零问题；Console 开启未使用局部变量和参数检查且构建通过；OpenAPI 生成无漂移；Go 全量测试、`go vet`、Console 测试、生产构建和真实三设备冒烟通过。Warm Pool 显式缩容仍可用，健康故障仍不自动删除或补建；正式库继续保持零历史污染，三台 Device ID/Provider ref 不变。

### DF-058 独立控制台安全保持登录

实施：复用现有 PostgreSQL Console Session、HttpOnly/SameSite Cookie、CSRF 双提交校验、Argon2id 配置用户和注销吊销机制，把默认绝对会话期限与空闲期限统一设为 30 天。登录页明确说明只保存安全登录状态、不在浏览器记录明文密码；账号、角色和密码哈希来源不变，不新增表、API、状态或第二套认证实现。

产出：长期会话默认配置、登录页安全提示、配置/前端/认证回归、正式部署验证和 `docs/evidence/DF-058/acceptance.md`。

验收：新登录响应下发 30 天持久 Cookie，数据库 `expires_at` 与配置一致；复用 Cookie 在浏览器重开及超过旧 30 分钟空闲窗口后仍可访问，主动退出后立即失效；浏览器存储、静态资源、日志、证据和 Git 中没有明文密码。Go 全量测试、静态检查、Console 测试和生产构建通过；正式 Server 与 iOS 隧道重建后 Android 和两台 iOS 逐台完成真实 Appium `/source`，历史测试记录再次清零，随后只删除已确认不再使用的部署备份、退出回滚容器和旧 Server 镜像。

### DF-059 修复正式控制台密码哈希并完成真实登录验收

实施：纠正 DF-058 只验证会话配置、却沿用旧 Secret 哈希并误判账号可登录的验收遗漏。根据管理员明确指定的密码重新生成 Argon2id 哈希，只替换正式只读 Secret volume 中 `admin` 的 `password_hash`；用户名、显示名和角色不变。重启 Server 清除进程内错误登录限流，并同步重建依附 Server 网络空间的 iOS 隧道。明文不得进入代码、Git、证据、远端临时文件或日志。

产出：正确的正式 Argon2id Secret、真实 HTTPS 登录/当前会话/30 天 Cookie/注销验证、零临时文件与零验证数据、`docs/evidence/DF-059/acceptance.md`。

验收：正式 HTTPS 登录返回 201，使用返回的 Secure/HttpOnly Cookie 查询当前会话返回 200，Cookie 到期时间为 30 天，注销返回 200；验证后 Console Session、Audit Event、Reservation 和 Device Session 均为 0。Secret 文件保持 `0400`、owner `65532:65532`；Server `running/healthy`，iOS 隧道 `running`、restart count 0，4811/4842 实际可达。

### DF-060 设备可读名称与指定设备排队

实施：为 Device 增加可编辑显示名称，并把编辑入口合入现有设备详情；列表、设备池模板和预约引用以名称为主，完整 ID 只放详情。北向普通预约增加可选 `requested_device_id`，校验目标属于请求 Pool 后复用既有 Scheduler 精确分配；目标使用中时 Reservation 保持 pending，释放后自动调度到同一设备，禁止回退到 Pool 内其他设备。Alcor 通过 Adapter 传递明确 Device，不复制设备或预约数据。

产出：设备名称迁移与管理 API、Console 详情编辑、指定设备预约契约、Scheduler/API/前端测试、正式部署和 `docs/evidence/DF-060/acceptance.md`。

验收：管理员可在设备详情编辑 2～40 字符名称且所有当前设备域引用刷新为新名称；页面没有单独“改名”操作；指定空闲设备只分配该设备；指定使用中设备保持 pending，原预约释放后自动获得该设备；不属于 Pool、已删除/停止/隔离的目标被拒绝；未传目标字段的旧调用继续按 Pool 调度。Go 全量测试、Console 测试和生产构建通过，30.171 部署健康且无活动预约被中断。

### DF-061 修复跨入口挂断与过期预约回收

实施：修复 Alcor 服务入口代用户创建人工预约后，独立 Console 因 `client_id` 不同而无法由同一用户挂断的问题；管理员跨预约归属释放时由 Console 显式提交 `force=true`，继续复用既有权限和强制释放审计。修复 Host/Reconciler 已把设备恢复为 `ready` 后，Release/Reaper 重复执行 `ready -> ready` 并因数据库受影响行检查失败而让预约永久保持 active 的问题。不得放宽非本人普通释放、operator/viewer 强制释放或设备状态机的其他边界。

产出：Reservation Service/Repository 幂等收敛修复、管理员 Console 释放参数、PostgreSQL 集成测试、Console 组件测试、30.171 正式备份与真实远控申请/挂断回归，以及 `docs/evidence/DF-061/acceptance.md`。

验收：同一 Console 用户可释放由 Alcor 可信网关代建的 manual Reservation；非本人普通释放仍返回 forbidden；管理员跨归属释放进入 `force_released` 并记录 `force_release_device_reservation`；过期 active Reservation 即使绑定设备已是 `ready`，Reaper 仍关闭 Session、把 Reservation 置为 expired 并保持设备 `ready/healthy`。Go 全量测试、`go vet`、Console 全量测试和生产构建通过；正式卡住预约自动收敛，新增 Android 远控申请/连接/明确挂断完整通过，STF 与 iOS 隧道无回归。

### DF-062 修复 Emulator OOM 后的幽灵占用与原机恢复

实施：延续 ADR-0029 的非破坏恢复约束，补齐 Docker `State.OOMKilled` 解析与 Provider 错误分类；Agent 将仍在运行但未通过 ADB、启动或 Appium 完整检查的 Emulator 上报为 `booting/unhealthy`，并把运行时 `docker` 映射为设备域 `docker_emulator`。Reconciler 对没有 create/rebuild 在途命令的空闲 `booting/unhealthy` 设备累计失败并隔离，随后只复用既有 restart Host Command。STF 对 inventory 已不存在或 `present=false` 的设备把重复 release 视为幂等完成，仍拒绝释放 `present=true && using=true` 的真实占用。修复 Docker 模式没有 iOS Session Fence 时 typed nil 被误执行造成的 Agent panic。

产出：OOM 状态解析、Agent/Reconciler 健康收敛、STF 幽灵占用释放、Docker Agent 启动兼容回归、30.171 正式 Server/Agent 部署和 Android 真实 Appium 验收，以及 `docs/evidence/DF-062/acceptance.md`。未新增 API、表、migration、配置或自动 delete/rebuild/reimage/create。

验收：正式过期 Reservation 和 Session 自动关闭；同一 Android Device ID、Provider ref、Pool membership、容器与数据卷保持不变；系统只产生一次 `operation_source=self_healing` 的 restart，破坏性命令为零。重启后 Docker `OOMKilled=false`，ADB、Android boot、STF 与 Appium `/source` 均通过，设备最终为 `ready/healthy`，开放 Reservation、Session 和在途 Host Command 均为零。Go 全量测试、`go vet`、真实 PostgreSQL Reconciler 集成测试和正式部署检查通过。

### DF-063 Android CPU 和内存无损改配

状态：completed。

实施：按 ADR-0030 新增只接受容器/Android CPU 和内存的受控 Device API。Server 复用动态容量、PostgreSQL Host Command、幂等和审计；Agent 复用 Docker Provider `RestartWithProfile` 保留数据卷替换容器，并在 ADB、STF、Appium 全部健康后提交有效规格。目标失败时用同一数据卷恢复旧规格一次。Console 将“调整 CPU/内存”与“更换镜像/重建数据”分开；镜像、数据盘和图形模式继续走清空数据的 reimage。

产出：ADR-0030、migration、OpenAPI、Management/Agent/Host Command 编排、Console 分流、自动化测试、`10.0.30.171` 真实 Linux KVM 验收和 `docs/evidence/DF-063/acceptance.md`。

验收：纯 CPU/内存修改不删除数据卷，Device ID、Pool membership、APK、应用数据和文件保持；Docker 限额与 Android Guest 参数更新；使用中、存在在途命令、非法规格或容量不足时在替换前拒绝；目标失败恢复旧规格，恢复失败隔离；镜像或数据盘修改仍明确清空；Go 全量测试、静态检查、Console 测试和生产构建通过，正式 Server/Agent/Console 发布后设备回到 `ready/healthy`。

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
