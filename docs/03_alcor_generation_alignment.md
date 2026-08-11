# 新版 Alcor、旧版 master 与设备农场方案三方对齐

## 1. 三个输入分别代表什么

| 输入 | 作用 | 是否可直接作为新接口依据 |
|---|---|---|
| 本地 `E:/AutoTestTools/Projects/Alcor` 的 `master`，commit `1755f5c58e0b20deff2e9c331457c0933fcdc9de` | 当前旧版代码事实和迁移来源 | 否；只能用于理解历史数据和现有执行器 |
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
4. 请求携带 `owner_type=run_attempt`、RunAttempt UUID/ULID、幂等键、设备能力和租期；`manual/test_run` 只用于人工调试和 DaFit 联调；
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

在新版 Alcor 第六阶段接口完成前，可以安全开发：设备表和状态机、Host Agent、Provider、Scheduler、Reservation、Reaper、Reconciler、STF/Appium Adapter、OpenAPI、Mock Provider、DaFit Harness、契约测试、故障测试，以及只调用设备 API 的 Device Farm Console。Console 可使用独立配置用户和设备域短时会话，并为管理员编排与 Reservation 绑定的 STF 原生 Web 入口，但不得复制 Alcor 钉钉用户、平台 RBAC、业务审计模型或 STF 远控实现。

当前不能安全定稿：新版 Eval Console 如何链接、嵌入或复用 Device Farm Console、Android Case/Template 结构、App/APK Build 正式模型、Worker 内 Android Executor 代码位置、Device Farm Adapter 的具体 Go 接口。它们必须等待新版实际分支或专项接口文档，不能根据旧 master 猜测；这不阻塞 Device Farm Console 独立交付。

## 6. 接入前检查点

1. 获取新版 Alcor 实际开发分支 commit，而不是继续使用本地旧 master 推断；
2. 核对 `RunAttempt` 主键类型、状态、租约和取消语义；
3. 核对 `/api/v1` 统一响应、认证方式和服务间凭证；
4. 核对 ArtifactStore 上传责任和 Supabase object key；
5. 核对关联 Header 和审计 Actor 的传递方式；
6. 用双方 OpenAPI 做契约测试后再开始真实接入；
7. 任何不一致先更新本文件和 ADR，不在 Adapter 中堆临时兼容分支。
# DF-035 官方目录与按需准备补充（2026-08-10）

Android System Image 的版本、映像类型和 ABI 由 Android SDK 官方稳定频道提供；CPU、内存、数据盘、分辨率、DPI 和图形模式属于本设备域的 `runtime_profile`。Device Farm Console 只调用 Server 的目录和准备任务 API，既不访问 Google，也不获取下载链接、Registry 凭证、Host 命令或 Docker Socket。Server 仅编排受控 Build Agent；只有 Agent 完成构建、内部 Registry 推送、不可变 digest 获取和现有验证链路后，才创建或更新 `device_images`。这仍是设备域基础设施准备，不形成新版 Alcor 的 Image/Artifact 业务索引或 Run 队列。
