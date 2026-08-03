# ADR-0004：设备农场接入新版 Alcor Run/Worker 架构

## 状态

已确定。

## 背景

设备农场方案编写时，本地 Alcor master 仍使用 `test_items`、`datasets`、`eval_tasks`、`eval_results`、整数 ID、进程内 goroutine Runner 和本地任务目录。随后提供的新版 Alcor 方案明确进行一次性重构：使用 Case、Run、RunAttempt、独立 Worker、ClickHouse、Supabase Storage 和 Eval Console，并把 Device Farm 放在第六阶段通过 Adapter 扩展。

## 决策

- 设备生命周期、调度、预约、STF、Appium、Host Agent 和真机扩展继续遵循设备农场方案；
- Alcor 平台对象、ID、API、存储、前端、队列和接入方式遵循新版 Alcor 方案；
- 当前设备农场保持独立设备域和北向 API，新版 Worker 通过 Device Farm Adapter 调用；
- 新版 Alcor 预约 Owner 使用 `owner_type=run_attempt` 和 RunAttempt UUID/ULID；人工调试和 DaFit 联调分别使用受控的 `manual/test_run`；
- 正式结果、报告和日志由 Alcor 写入 ClickHouse/Supabase Storage，设备农场不复制；
- 本地旧版 master 只作为迁移来源和现有评估器参考，不作为新设备接口依赖；
- 新版方案尚未定义的 Android Case、App/APK Build 和 Adapter 代码位置暂不定稿。

## 覆盖关系

本 ADR 优先覆盖 ADR-0003 和设备农场方案中以下旧平台假设：`alcor_console`、旧表复用、整数 Task ID、本地 ArtifactStore、固定 migration 编号以及设备代码必然迁入旧 Alcor 目录。它不覆盖设备农场内部功能设计。

## 后果

- 设备域可以在新版 Alcor 开发期间继续推进；
- 将来接入点稳定在 RunAttempt → Device Farm Adapter → Reservation，不需要兼容旧 Eval Task；
- 不会产生第二套 Run、Result、Artifact 或 Eval Console；
- 接入前必须获得新版实际开发分支并执行 OpenAPI 契约测试。
