# 新版 Alcor、旧版 master 与设备农场方案三方对齐

## 1. 三个输入分别代表什么

| 输入 | 作用 | 是否可直接作为新接口依据 |
|---|---|---|
| 本地 `D:/AutoTestTools/Projects/Alcor` 的 `master`，commit `1755f5c58e0b20deff2e9c331457c0933fcdc9de` | 当前旧版代码事实和迁移来源 | 否；只能用于理解历史数据和现有执行器 |
| `reference/alcor_next_generation_plan.txt` | 正在开发的新版 Alcor 目标 | 是；平台对象、接口、存储、前端和集成边界以此为准 |
| `reference/app_evaluation_device_farm_design.md` | 设备农场完整功能、状态机、调度、STF/Appium 和真机扩展设计 | 是；设备域内部能力以此为准，但其旧 Alcor 假设被新版方案覆盖 |

本地 master 还有未跟踪的 `alcor_console/` 和 `docs/`，它们不属于 commit `1755f5...`，本次没有修改，也不能据此认定新版前端已经进入 master。`Alcor/docs/app_evaluation_device_farm_design.md` 与本项目归档方案的正文语义一致，差异是前者内嵌 Mermaid、后者引用五张 PNG；本项目继续以用户提供的图片版原文和哈希为归档基线。

## 2. 已确认的版本差异

| 主题 | 本地旧版 master | 设备农场原方案中的假设 | 新版 Alcor 目标 | 本项目采用 |
|---|---|---|---|---|
| 前端 | 无已跟踪前端；本地只有不可作为源码复用的未跟踪构建产物 | `alcor_console` | `eval_console` | 本仓库新增只负责设备域的 Device Farm Console；未来可接入 Eval Console，但不实现评估业务页面 |
| 用例 | `test_items`，整数 ID，历史字段 | 复用 `test_items` | `cases` + Protocol Template，UUID/ULID | 不依赖旧表；未来只接新版 Case 语义 |
| 数据集 | `datasets/dataset_items` | 扩展旧 Dataset | PostgreSQL 元数据 + 不可变版本 + ClickHouse 明细 | 设备农场不保存 Dataset |
| 任务 | `eval_tasks` | Eval Task + Attempt | `runs` + `run_attempts` | Reservation Owner 指向 RunAttempt UUID/ULID |
| 重试 | 删除旧结果并重跑 | 新建 Attempt | `POST /runs/:id/attempts` | 只认新的 RunAttempt，不操作结果 |
| 状态 | pending/running/completed/failed，代码还有 success 差异 | queued/preparing/running/success... | queued/preparing/running/passed/failed/canceled/timeout/infra_failed | Alcor 业务状态按新版；设备状态/预约状态独立命名 |
| 执行 | API 内 goroutine 调用 Python | 新增 Worker | 独立 Worker + PostgreSQL 租约 | Device Farm 不建 Run Worker；只向 Worker 提供设备 |
| 报告和文件 | 本地 `/tasks`、`/assets` | 本地 ArtifactStore 起步 | Supabase Storage + ArtifactStore | 设备农场不存业务报告，正式产物由 Worker 上传 |
| 大规模明细 | PostgreSQL | PostgreSQL 为主 | ClickHouse 保存数据集明细、用例结果和指标 | 设备农场不复制 ClickHouse 数据 |
| 用户审计 | 无完整钉钉/RBAC | 后续新增 | 钉钉登录、users、audit_logs | 设备服务只鉴别服务/Agent；操作人由 Alcor 记录 |
| Device Farm 集成 | 不存在 | 直接在 Alcor 增加 devicefarm/agent/migration | 第六阶段，通过 Worker 的 Device Farm Adapter | 当前独立开发 API；未来 Adapter 对接，不先写死代码迁移 |
| 可观测性 | 普通服务日志 | 设备事件和审计 | Run/Attempt Header + traceparent + HyperDX MCP | 设备 API 和日志透传三个关联标识；`/metrics` 只输出设备基础设施聚合状态，不复制业务指标 |

## 3. 对设备农场方案的有效性判断

### 继续有效

- 统一 Device 抽象，模拟器和未来 USB 真机使用同一上层模型；
- Host Agent、Docker Emulator Provider、Image、Host、Pool、Device、Reservation；
- Scheduler、PostgreSQL 并发约束、租约、Reaper、Reconciler；
- STF 只做远控和可见性，不能作为设备占用真相；
- Appium 2/UiAutomator2 复用，设备农场只管理 Endpoint 和健康；
- 超时、重试、隔离、重建、人工释放和审计事件；
- 控制台设置 Pool 总目标后，由设备域按 Host 实际 CPU、内存、磁盘和设备规格自动扩容或安全缩容；
- Android 13～16 镜像目录、Image 默认运行规格、Device 规格覆盖和空闲 Emulator 受控重装；
- 隔离或已停止设备可由 Device Farm 管理员通过设备域 Host Command 受控删除；该动作不创建 Alcor Run/Result，也不绕过目标容量；
- Android CPU/内存无损改配属于 Device 管理运维动作，只更新设备域有效规格；新版 Alcor 不新增对应业务对象，也不感知 Host Command、Docker 容器或数据卷；
- 受管 iOS Simulator 确定故障且没有活动占用时，可由设备域自动复用相同删除链清理并按 Pool 原目标补建；该自愈不改变 Alcor Reservation/RunAttempt 语义，不产生业务结果；
- `/api/v1/device-*` 和 `/internal/v1` 的资源化接口方向；
- 后续接真机只增加 Provider，不重做调度、预约和执行链路。
- 设备总览、镜像、Host、Pool、Device、Reservation 属于设备域，可以由独立 Device Farm Console 管理；STF 原生 Web 页面按 ADR-0013 由设备域短租约和短时授权受控打开，Console 仍不展示 `remoteConnect` TCP 地址。

### 被新版 Alcor 覆盖

- Alcor 评估业务前端从 `Alcor Console/alcor_console` 改为 `Eval Console/eval_console`；Device Farm Console 按 ADR-0009 独立交付，不沿用旧 Alcor 业务页面；
- `test_items/eval_tasks/eval_results` 不再是新系统对象；
- 顶层整数 Task ID 不再作为设备预约关联 ID；
- 本地任务目录和报告 URL 不再是正式 Artifact 方案；
- 原方案固定的 `000012...000021` migration 编号不能直接沿用；
- 把设备域代码必然迁入旧 `eval_server/internal/devicefarm` 的结论不再成立；
- PostgreSQL 单独承载所有大规模用例明细和结果的假设不再成立；
- App、Build 等正式业务模型需等待新版 Alcor 的 Android/Device Farm 扩展方案，不在设备项目提前建表。

## 4. 新版接入契约

### Alcor 调用设备农场

1. Eval Console 创建 Run；
2. 独立 Worker 领取 RunAttempt；
3. Device Farm Adapter 调用 `POST /api/v1/device-reservations`；
4. 请求携带 `owner_type=run_attempt`、RunAttempt UUID/ULID、幂等键、设备能力和租期；当用户在 Alcor 选择具体设备时同时携带 `requested_device_id`，设备农场只为该设备分配，使用中则保持 pending 等待释放；`manual/test_run` 只用于人工调试和 DaFit 联调；
5. 同时透传 `X-Eval-Run-Id`、`X-Eval-Attempt-Id` 和 `traceparent`；
6. 设备农场返回 reservation、device、UDID、Appium Endpoint 和受控 STF 远控信息；
7. Worker 执行 Android 自动化，将用例结果写入 Alcor/ClickHouse，将报告和日志上传 Supabase Storage；
8. Worker 在成功、失败、取消、超时和基础设施失败路径都释放预约。

### 设备农场不得做

- 查询或修改 Alcor 的 `runs/run_attempts/run_results` 表；
- 保存 Case、Dataset、Target、Config 或业务 Secret；
- 生成 Alcor 的门禁、LLM 报告或业务评分；
- 保存 Supabase 管理员密钥；
- 把 DaFit report.json 变成第二套正式 RunResult；
- 依赖旧版 `/api/v1/eval-tasks` 或 `/tasks/{id}/report.html`。

## 5. 当前可安全开发的范围

在新版 Alcor 第六阶段接口完成前，可以安全开发：设备表和状态机、Host Agent、Provider、Scheduler、Reservation、Reaper、Reconciler、STF/Appium Adapter、OpenAPI、Mock Provider、DaFit Harness、契约测试、故障测试，以及只调用设备 API 的 Device Farm Console。Console 可使用独立配置用户和设备域短时会话，并为 operator/admin 编排与 Reservation 绑定的 STF/Baguette 原生 Web 入口，viewer 保持只读；不得复制 Alcor 钉钉用户、平台 RBAC、业务审计模型或远控实现。

新版 Alcor 实际 `test` 分支已具备钉钉会话、App Build、Android Worker 和 Device Farm Adapter，因此统一入口确定为：Alcor“设备农场”一级菜单展开运行概览、宿主机、设备池、设备、预约、应用版本、健康事件和操作审计二级入口；设备域页面由同源受控代理按路由嵌入既有 Device Farm Console，APK 版本继续属于 Alcor，Device/Reservation/远控继续属于设备农场。浏览器只携带 Alcor 会话；Alcor 服务端持有 Service Token 并透传受控操作者 ID。仍未定稿的范围只剩 Android Case/Template 的进一步产品化和平台 RBAC；不得用当前统一入口复制设备域数据或页面。

## 5.1 DF-038 长期设备和基础设备扩容补充

DF-038 仍只改变 Device、Pool、Reservation、Host Command、持久化 provisioning job 与设备域审计：系统镜像继续作为受控基础设施缓存，创建向导仅提交目录项，Server 自动触发或复用准备并在验证后继续创建，但不新增 Alcor App、Build、Run 或 Artifact。Pool 的基础设备只提供 Phone Profile、已验证 Image 和 runtime profile 给后续干净 Emulator 创建；不复制业务 APK、账户、缓存或数据卷。Reservation release 仅释放 STF 和数据库占用，设备直接回到 `ready/healthy`，只有显式 rebuild/reimage 或 delete 才清空 Provider 数据。管理员直接删除空闲设备会原子降低 Pool 目标以避免自动补回。

## 5.2 DF-056 长期设备非破坏恢复补充

DF-056 只改变设备域内部的健康收敛和运维数据：系统自动隔离的 Android Device 可复用 Agent heartbeat、STF 健康和既有 Provider `Restart` 恢复原 Device，不能创建 Alcor Run/Result，也不能触发 DaFit 业务 App 重置。隔离设备继续占用 Pool 登记容量，自动恢复失败只形成设备域告警；delete/rebuild/reimage 必须由设备管理员明确操作。正式启用前清理的测试 Reservation、Session、健康事件、审计和命令不属于 Alcor 业务数据，且不得删除当前 Host、Pool、Image、Device 或 Provider 资源。

## 5.3 DF-058 独立控制台保持登录补充

DF-058 只调整独立 Device Farm Console 的设备域技术会话期限，不改变 Alcor 钉钉会话、Service Token、角色边界或设备 API。浏览器继续只持有 HttpOnly/SameSite Cookie 和非 HttpOnly CSRF 双提交 Cookie，不保存明文账号密码；Alcor 同源代理模式仍以 Alcor 会话为准，不复制这套独立 Console 登录。

## 6. 接入前检查点

1. 获取新版 Alcor 实际开发分支 commit，而不是继续使用本地旧 master 推断；
2. 核对 `RunAttempt` 主键类型、状态、租约和取消语义；
3. 核对 `/api/v1` 统一响应、认证方式和服务间凭证；
4. 核对 ArtifactStore 上传责任和 Supabase object key；
5. 核对关联 Header 和审计 Actor 的传递方式；
6. 用双方 OpenAPI 做契约测试后再开始真实接入；
7. 任何不一致先更新本文件和 ADR，不在 Adapter 中堆临时兼容分支。

## 7. Android 第一版归档与第二版边界

Android 第一版冻结在 `master@106e9dd` 和 Tag `archive/android-baseline-2026-08-17`；本地第二版使用 `codex/device-farm-v2`，详见 ADR-0020。分支切换不改变三方职责：设备农场仍只保存 Device/Host/Pool/Reservation 与技术连接真相，Alcor 仍保存 App/Build/Run/RunAttempt/Result/Artifact 业务真相，DaFit 仍是 Android 业务执行复用来源。

DF-039 对 iOS 只进行设计和复用验证。iOS Device、Host、Pool、Reservation 和技术连接属于设备域；IPA/App Build、iOS Case、执行结果和业务报告属于 Alcor 或对应执行器。Appium Device Farm 即使提供 Hub/Node、设备发现和 Session 路由，也不得覆盖我方 PostgreSQL 预约或独立维护另一套业务设备池；真实 Session 必须与我方已预约的明确 UDID 对齐。

DF-039 通过后，iOS 北向关系按 ADR-0021 固定：Alcor 仍先以 RunAttempt 创建、续租和释放 Reservation；设备农场返回明确平台、Host、Device、UDID 和受控 Session 连接，不返回 Apple Secret。iOS Executor 必须同时使用服务端生成的单值字符串 `df:udids=<reserved_udid>` 与相同 `appium:udid`，不得提交 tags、host filter 或多 UDID 让插件再次选机。Appium Device Farm 内部 busy 只用于宿主机技术互斥，不能创建、延长或关闭 Alcor RunAttempt，也不能覆盖 PostgreSQL Reservation。

iOS Executor、IPA 元数据提取、安装、Case、结果和 Artifact 是 Alcor/执行器后续任务；本仓库的 DF-040～DF-046 只建设平台中立设备域、macOS Host、iOS inventory/health、Session Fence、Console 设备页和真实基础设施验收。Android DaFit Executor、STF 和长期设备语义必须保持不变。

DF-042 按 ADR-0022 固定具体接入：Alcor 可信 iOS Executor 先用 Service Token 为自己的 active Reservation 申请一次性 Session Grant，再把 Grant 交给对应 macOS Host 的 Fence。Grant 不是 Alcor Run Token，不写入 RunAttempt、日志或 Artifact；Fence 只限制明确 UDID 和转发既有 WebDriver 协议。Alcor 浏览器、Device Farm Console 和普通用户都不能获得 Grant、Fence Endpoint 或 Appium Node Endpoint。现有 Alcor Device Farm Gateway 的服务端持证、固定路径白名单和关联 Header 透传模式可以复用，但 iOS Executor 与 XCUITest 业务实现仍留在 Alcor。

DF-046 的 WDA 人工远控已由 ADR-0026 和 DF-050 取代。人工远控仍属于设备域运维能力，不是 Alcor 业务执行器；它复用同一 Reservation 和明确 UDID，通过 Baguette Adapter 挂载上游原生 Web UI，不创建人工 Appium Session。Alcor 浏览器只获得签名 Gateway 入口，不获得 Mac/Baguette 回环地址、Host/Fence/Appium/WDA 地址或 Grant，也不新增 Run、RunAttempt、结果和 Artifact 语义。macOS VNC 只可作为宿主机运维能力，不能作为设备用户远控入口。

DF-043 的固定库存接入继续作为发现、启动/停止、预约和 Session 基础。ADR-0024 根据需求方确认取代其禁止创建/删除的首期限制：DF-044 起允许 Server 通过既有 Host Command、Agent 和 Provider 对 Host 已安装且受控的 CoreSimulator Runtime 执行动态 create/boot/shutdown/erase/delete。该能力仍只产生 Device/Pool/命令/审计，不产生 Alcor Run/Result；Appium Device Farm 继续只提供 inventory、技术 busy 和明确 UDID Session 路由。
# DF-035 官方目录与按需准备补充（2026-08-10）

Android System Image 的版本、映像类型和 ABI 由 Android SDK 官方稳定频道提供；CPU、内存、数据盘、分辨率、DPI 和图形模式属于本设备域的 `runtime_profile`。Device Farm Console 只调用 Server 的目录和准备任务 API，既不访问 Google，也不获取下载链接、Registry 凭证、Host 命令或 Docker Socket。Server 仅编排受控 Build Agent；只有 Agent 完成构建、内部 Registry 推送、不可变 digest 获取和现有验证链路后，才创建或更新 `device_images`。这仍是设备域基础设施准备，不形成新版 Alcor 的 Image/Artifact 业务索引或 Run 队列。

# DF-036 镜像生命周期与默认选择补充（2026-08-11）

旧 Device Image 的清理属于设备域运维：只有不再作为任何 Pool 默认镜像、且只被 `deleted` 历史 Device 引用时才能受控停用。停用记录默认不进入 Console 和新版 Alcor 的可选列表，但继续保留设备域审计及历史外键。管理员在 Image 页面选择已验证 Image 作为某个 Pool 的默认值时，只改变后续自动补建选择，不创建 Alcor Image/Artifact，不隐式重装现有 Device，也不让 Server 或浏览器直接操作 Registry/Docker。

# DF-037 Phone 硬件模板和受控创建补充（2026-08-11）

DF-037 只扩展 Device Farm 的基础设施创建入口：管理员在 Device Farm Console 选择 Phone 硬件模板、官方 System Image、Pool 和运行规格，Server 以持久化 `create` Host Command 编排已有 Agent/Provider 链路。设备创建状态、Pool membership、Host 容量、镜像准备和健康检查仍是设备域真相；不新增 Alcor App、Build、Case、Run、RunAttempt、Result 或 Artifact 模型。首期不实现 Phone 以外的 Android form factor，未来扩展必须另行更新复用矩阵与验收项。

DF-037 容量反馈补充：持久化创建任务允许使用 `waiting_capacity` 技术状态和结构化 `capacity_result` 保存宿主机实时容量缺口。该状态不创建 Alcor Run/Result，不把等待任务误标为失败；Server 继续按现有 Reconciler 重试，容量恢复后清除缺口并进入既有 Host Command 创建链路。Device Farm Console 只显示中文资源原因和缺口数值。
