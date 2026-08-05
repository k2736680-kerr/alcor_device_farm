# 开发实施计划

具体功能以 [MVP 功能方案](04_mvp_functional_spec.md) 为准；实际编码按 [逐步实施清单](05_step_by_step_implementation.md) 的 DF 编号执行；阶段和最终验收按 [MVP 验收方案](07_acceptance_test_plan.md) 执行。本文件只说明总体阶段关系和防跑偏检查，不替代逐项任务。

## 与两份方案阶段的关系

- 设备农场方案中的设备 Host、Provider、Pool、Reservation、Scheduler、STF、Appium、Reconciler 和 Reaper 仍是本项目功能范围；
- 新版 Alcor 方案第一至第五阶段当前不实现 Device Farm，第六阶段才通过 Worker 的 Device Farm Adapter 接入；
- 本文阶段一至阶段四用于在等待新版 Alcor 时完成设备域和 DaFit 真实联调；
- 本文阶段五交付可独立使用的 Device Farm Console；
- 本文阶段六必须以新版 `Run/RunAttempt/Artifact` 契约接入，不再扩展旧版 `eval_tasks/eval_results`；
- 原设备方案中关于旧 Alcor 表、整数 ID、本地 ArtifactStore、`Alcor Console` 和 migration 编号的内容不再作为接入依据。

## 阶段一：契约与基础骨架

- 逐项执行现有能力复用矩阵，禁止未经审查的新实现；
- 固化设备域边界和术语；
- 定义北向 OpenAPI 和 Agent 内部协议；
- 北向契约使用 UUID/ULID 外部 Owner ID，并兼容 `request_id/data/error` 响应；
- 日志和调用链透传 `X-Eval-Run-Id`、`X-Eval-Attempt-Id` 与 `traceparent`；
- 建立只含设备域的 PostgreSQL 开发模型，表名、字段、状态和约束与确定方案兼容；
- 实现统一错误结构、request ID 和幂等键；
- 实现 Mock Provider 和设备状态机单元测试。

验收：在没有 Alcor、STF、Docker 和真实设备时，设备、设备池和预约状态机可以自动化测试。

## 阶段二：Host Agent 与 Docker Emulator

- Agent 注册、心跳、排空和命令长轮询；
- Docker Emulator 创建、启动、停止和重建；
- ADB online、boot completed、Appium 健康检查；
- 镜像不可变 digest 和资源容量限制；
- Linux KVM 环境部署验证。

验收：同一镜像可重复创建干净设备，设备达到 ready 后可建立 Appium Session。

说明：本阶段只管理 Appium 服务和 Endpoint，业务 WebDriver 执行继续复用 DaFit 现有实现，未来由 Alcor Executor 接管。

## 阶段三：STF、设备池和预约

- STF + RethinkDB 部署；
- STF inventory、claim、release、remoteConnect；
- 单一默认逻辑设备池、参数化固定目标自动补齐和能力筛选；
- PostgreSQL 事务预约和 active 唯一索引；
- 续租、主动释放、Reaper 和 Reconciler；
- 离线、隔离和重建。

验收：当前单台模拟器不能双占，第二个预约不突破容量；并发正确性由两设备 PostgreSQL 集成测试覆盖，服务或 Agent 重启后状态可以收敛。

## 阶段四：DaFit 真实联调

- 在 `dafit_auto_platform` 创建 `feature/device-farm-adapter`；
- 增加 Farm 运行模式，不改变现有本地模式；
- Harness 申请设备并注入 UDID、Appium Endpoint 和独立报告目录；
- 执行一个冒烟用例，收集报告后释放设备；
- 验证下一次预约无法读取上一次任务数据。

验收：申请设备、执行 DaFit、保存报告、释放和重建全链路无人值守完成。

## 阶段五：Device Farm Console

- 使用 React + TypeScript 建立本仓库的设备控制后台；
- 只调用 `/api/v1/device-*`，不直连数据库、Docker、STF、ADB 或 Appium；
- 提供设备总览、Image、Host、Pool、Device、Reservation 和设备域审计页面；
- 提供人工预约、续租、释放、隔离、重建和受控 STF 远控入口；
- 浏览器使用安全会话或受信任反向代理，不能持有 Service Token 或 STF 管理 Token；
- 使用 Mock 和真实单台 Android 16 环境完成浏览器端到端测试；
- 纳入部署、升级、回滚、监控和安全验收。

验收：用户不使用命令行即可通过 Web 完成设备查看、预约、远控和释放；危险操作有确认、原因和审计；页面状态始终以 Server/PostgreSQL 为真相；控制台没有 Alcor 评估业务对象。

## 阶段六：Alcor 接入

- 以新版 Alcor 实际开发分支和 OpenAPI 为准生成/实现 Device Farm Adapter；
- Alcor Worker 使用 RunAttempt UUID/ULID 调用设备预约接口；
- 将 Device Session 与 RunAttempt 关联；
- Worker 使用返回的 ADB/Appium Endpoint 执行业务用例；
- Alcor 将业务元数据写 PostgreSQL、用例级结果写 ClickHouse，并通过 ArtifactStore 上传 Supabase Storage；
- 终态和异常路径调用释放接口；
- Alcor API/Worker 与设备农场进行契约和故障注入测试；
- Eval Console 后续可以链接、嵌入或复用 Device Farm Console 的设备域模块，也可以继续通过 Adapter 调用同一设备 API；具体方式等待新版实际前端确定。

验收：Eval Console 是统一评估业务入口；Device Farm Console 继续是设备域独立入口；RunAttempt 与 Device Session 可追溯；Alcor 与设备农场各自只保存所属领域真相；报告进入 Supabase Storage、用例结果进入 ClickHouse；不存在旧版 Eval Task 依赖、第二套评估业务模型或共享数据库耦合。

## 每阶段防跑偏检查

每一阶段开始和结束都必须逐项核对：

1. 是否对应设备农场方案中的明确模块或能力；
2. 是否符合新版 Alcor 的 Case/Run/RunAttempt/Artifact 和 Device Farm Adapter 边界；
3. 是否违反 `docs/01_existing_capability_reuse_matrix.md` 的复用规则；
4. 是否意外实现了 Alcor 的 Case、Dataset、Target、Config、Run、RunAttempt、评分或报告业务；
5. API、外部 ID、状态和错误语义是否可映射到新版 RunAttempt；
6. 是否产生 STF、Appium、DaFit、Run 队列或业务制品的第二套实现；
7. 是否依赖旧版整数 Task ID、`eval_tasks/eval_results`、`/tasks` 本地报告或进程内 goroutine Runner。
8. 控制台功能是否只操作设备域，且浏览器没有持有 Service/STF Token 或访问内部基础设施端口。

任一项不满足，停止进入下一阶段，先修正文档、代码或补充经确认的 ADR。
