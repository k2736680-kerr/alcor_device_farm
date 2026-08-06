# 现有能力复用矩阵

本文件是编码前的强制检查表。目标是只建设缺失的设备域能力，不产生第二套 Appium、STF、评估任务、测试执行或报告实现。

2026-08-05 前端复用盘点：当前仓库没有前端工程；本地旧版 Alcor `master` 没有已跟踪的前端源码，本地 `alcor_console` 只有未跟踪构建产物和依赖目录；DaFit 没有设备管理 Web 后台。因此没有可直接复用的控制后台源码，但 React + TypeScript 技术方向和现有设备 OpenAPI 可以复用。

## 1. 直接复用，不允许重写

| 能力 | 现有来源 | 本项目使用方式 | 禁止事项 |
|---|---|---|---|
| 浏览器远程看屏和操作 | DeviceFarmer/STF | `adapters/stf` 调用 STF 页面和 API | 不开发第二套远控页面、画面流或触控协议 |
| STF设备Inventory | DeviceFarmer/STF | 读取并映射 serial、present、ready、using | 不复制STF设备库作为业务真相 |
| STF claim/release/remoteConnect | DeviceFarmer/STF REST API | Adapter封装并增加超时、重试和错误分类 | 不重新实现相同设备控制协议 |
| Android UI自动化协议 | Appium 2 + UiAutomator2 | 使用现有服务和Driver | 不自研WebDriver协议或UiAutomator2 Server |
| Android Emulator容器基础 | Google Android Emulator Container Scripts与Android SDK | 固定上游版本并制作内部不可变镜像 | 不从零编写Emulator实现 |
| 数据库事务与唯一约束 | PostgreSQL | 预约、租约、状态和命令使用PostgreSQL | 不用内存锁代替数据库并发控制 |

## 2. 复用 DaFit 已有实现，不迁入本项目

| 能力 | 已有代码 | 本项目职责 |
|---|---|---|
| ADB命令封装 | `dafit_auto_platform/core/adb` | 不复制；Harness只传入明确UDID，设备Agent仅做基础设施级发现和健康命令 |
| Appium Session | `core/driver/appium_session.py` | 不创建业务WebDriver Session；只提供可访问的Appium Endpoint |
| App生命周期 | `core/driver/app_lifecycle.py` | 不重写；由DaFit Runner处理DaFit应用生命周期 |
| 元素定位和等待 | `core/element` | 不实现 |
| 点击、滑动、返回和前台恢复 | `core/actions` | 不实现 |
| 通用断言 | `core/assertions` | 不实现 |
| 截图、XML和证据策略 | `core/evidence` | 不实现；只保管Harness返回的制品路径 |
| Case/Run结果 | `core/results` | 联调阶段读取现有`report.json`，不建立第二套业务结果模型 |
| 用例目录、计划和执行 | `runner` | 通过现有入口执行，不复制Planner/Executor |
| HTML/JSON报告 | `reporting` | 直接保留为联调Artifact，不开发新报告生成器 |
| 正式完整入口 | `tools/run_full.py` | Farm适配分支仍调用同一入口，或增加同层薄入口 |

DaFit项目后续只增加Farm运行适配，不改变上述职责：外部指定UDID、Appium Endpoint和报告目录，禁止自动挑选其他设备。

## 3. 对接新版 Alcor，不提前建设

本地 `Alcor` master 是旧版代码事实；附件中的新版方案是正在开发的目标。旧表和旧接口只作为迁移来源，不再作为设备农场新接口的依赖。

| 能力 | 新版 Alcor 目标来源 | 当前处理 |
|---|---|---|
| 用例和数据集 | `cases`、`datasets`、`dataset_snapshots`、ClickHouse `dataset_case_rows` | 本项目不建表、不建 API |
| 运行与重试 | `runs`、`run_attempts`、`POST /api/v1/runs/:id/attempts` | Alcor 自动执行使用 `owner_type=run_attempt` 和 UUID/ULID `owner_id`；另允许受控 `manual/test_run` 预约 |
| 运行结果与指标 | PostgreSQL `run_results`、ClickHouse `run_case_results/run_target_metrics` | 本项目不计算、不保存业务结果和用例级指标 |
| 基础设施运行指标 | Alcor 继续使用自身可观测平台；设备农场只暴露 Prometheus `/metrics` | 仅包含 Server、数据库、设备、Agent、Reservation 和 Host Command 状态，不复制 Run/Result 业务指标 |
| 业务制品 | PostgreSQL `artifacts` 索引 + Supabase Storage | 正式接入由 Worker 上传；设备农场不保存业务报告 |
| 业务执行队列 | 独立 Worker + PostgreSQL 租约 | 设备农场不建立第二套 Run 队列；只管理设备 Reservation 租约 |
| Device Farm 接入 | Worker 的 `Device Farm Adapter（后续）` | 当前固化北向设备契约和 Mock；等待新版 RunAttempt API 后联调 |
| 用户、权限、审计 | 钉钉登录、`users`、`audit_logs`、Eval Console | 不预建 Alcor 用户模型；Device Farm Console 只实现设备域浏览器访问保护和设备技术审计，未来可接公司身份或由 Alcor 透传操作者 |
| Target、Config、Secret | `targets`、`configs/config_versions`、受限 YAML | 本项目不接收业务密钥，不复制 Target/Config 管理 |
| 统一响应和关联标识 | `/api/v1` 的 `request_id/data/error`，`X-Eval-Run-Id`、`X-Eval-Attempt-Id`、`traceparent` | 北向 API 兼容统一响应并透传关联标识 |
| 旧版历史对象 | `test_items`、`eval_tasks`、`eval_results`、本地 `/tasks` | 仅由新版迁移 CLI 处理；设备农场禁止依赖 |

## 4. 允许新建的设备域能力

这些能力在Alcor和DaFit当前代码中都不存在，是本项目的有效开发范围：

- Device Host与Host Agent协议；
-统一Device模型和状态机；
-Docker Emulator Provider；
-USB设备基础设施Provider；
-Device Image、每 Image 运行镜像选择与不可变摘要验证；
-Device Pool与容量；
-Reservation、Lease、续租和释放；
-Scheduler和数据库并发锁；
-Reconciler和Reaper；
-设备健康事件、隔离、恢复和重建；
-STF Adapter，包括官方 REST API 封装和 Host Agent 将动态 ADB Endpoint 注册到同机 STF ADB server；
-Appium Endpoint/端口/健康管理Adapter；
-面向未来Alcor的设备北向API；
-Device Farm Console，只展示和操作设备域资源；
-浏览器安全访问、页面权限和设备域操作审计衔接；
-仅用于端到端证明的DaFit Harness。

## 5. 名称相近但职责不同的能力

| 本项目能力 | 看起来相似的现有能力 | 不属于重复实现的原因 |
|---|---|---|
| Agent ADB发现与健康检查 | DaFit `core/adb` | Agent需要跨设备Inventory和宿主机健康；DaFit ADB只操作已经选定的单台业务设备。Agent只实现必要的`devices/getprop/shell-ready`白名单，不实现App业务动作 |
| Appium Adapter健康检查 | DaFit Appium Session | Adapter只确认服务可用、端口隔离和Endpoint，不执行页面步骤；WebDriver仍由DaFit或未来Alcor Executor创建 |
| PostgreSQL Reservation | STF claim | Reservation是跨进程业务占用真相和租约；STF claim是远控工具的技术占用。顺序固定为数据库预约成功后调用STF claim |
| Device Session | 新版 Alcor RunAttempt | Device Session只描述一次设备占用与技术连接；RunAttempt负责整个业务执行和结果。两者通过外部 UUID/ULID 关联，不互相替代 |
| Device Farm Console | 新版 Alcor Eval Console | 前者只控制设备资源并可独立运行；后者负责完整评估业务。未来可以通过链接、嵌入或模块复用统一入口，但当前不复制 Alcor 业务对象 |
| 控制台远控入口 | STF 原生 Web 页面 | 控制台只申请与 Reservation 绑定的短时入口并跳转或受控嵌入，不实现画面流、触控、日志或文件协议 |

## 6. 开发审查规则

每个新增模块必须在代码评审中回答：

1. 新版 Alcor 方案是否已经定义同一业务能力？
2. DaFit是否已经有同一执行能力？
3. STF/Appium/Android SDK是否已经提供？
4. 能否通过Adapter调用而不是复制？
5. 若必须新增，它是否属于第4节允许的设备域？
6. 是否误用了旧版 `test_items/eval_tasks/eval_results` 或本地报告路径？
7. 若属于控制台功能，是否只操作设备域资源，且浏览器没有接触 Service/STF Token 或内部基础设施端口？

无法回答或没有更新本矩阵时，不进入编码。
