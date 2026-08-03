# Alcor Device Farm

Alcor App 评估体系中的 Android 设备农场运行与管理子系统。

本仓库是新版 Alcor 的 Device Farm Adapter 尚未开发期间，设备域的独立开发与验证工作区。它不是第二套评估平台：Case、Dataset、Target、Config、Run、RunAttempt、结果、Artifact 和管理前端归新版 Alcor；本仓库只提供设备、设备池、预约、宿主机、模拟器、STF 和 Appium 基础设施能力。

设备农场功能基线见 [App 评估与设备农场系统设计方案](docs/reference/app_evaluation_device_farm_design.md)，新版平台边界见 [Alcor 新版开发方案](docs/reference/alcor_next_generation_plan.txt)。两者发生平台对象、接口、存储或前端归属冲突时，以新版 Alcor 方案为准，具体规则见 [三方对齐说明](docs/03_alcor_generation_alignment.md)。

## 当前开发关系

```text
alcor_device_farm（当前设备域先行开发工作区）
        ↓ 分配明确的设备和 Appium Endpoint
dafit_auto_platform（首个真实自动化联调负载）
```

## 新版 Alcor 接入关系

```text
Eval Console
    ↓ /api/v1
eval_server + PostgreSQL + ClickHouse + Supabase Storage
    ↓ RunAttempt / 独立 Worker / Device Farm Adapter
alcor_device_farm
    ↓
Device Scheduler / Host Agent / STF / Docker Emulator / Appium
```

新版方案把 Device Farm 放在第六阶段，并明确通过 Worker 的 Device Farm Adapter 扩展。因此现在先稳定设备北向 API，不依赖本地旧版 `test_items/eval_tasks/eval_results`；等新版 Run/RunAttempt 接口可用后再做契约联调。

## 当前建设范围

- Linux KVM / USB 设备宿主机与 Host Agent；
- Docker Android Emulator Provider；
- USB Android Provider 预留；
- STF Adapter，只调用现有 STF 能力；
- Appium Adapter，只管理服务 Endpoint 与健康状态；
- 统一 Device 模型和状态机；
- 设备池、预约、续租、释放与并发保护；
- Reconciler、Reaper、隔离与重建；
- 面向未来 Alcor 的北向 API；
- 使用 `dafit_auto_platform` 验证真实执行链路的联调 Harness。

## 当前不建设

- 新版 Alcor 的 Protocol Template、Case、Dataset、Target 和 Config；
- Run、RunAttempt 和业务 Worker 队列；
- RunResult、用例级结果、Artifact、评分、门禁和业务报告；
- 将 `dafit_auto_platform` 复制进本仓库；
- 重新实现 Appium WebDriver、页面动作、断言和报告；
- 独立设备管理 Console；设备远控复用 STF，未来管理页面接入 Eval Console；
- iOS、Kubernetes 和复杂弹性预测。

详细边界见 [项目范围与系统边界](docs/00_scope_and_boundaries.md)，设备方案对应关系见 [确定方案对齐表](docs/02_architecture_alignment.md)，新旧 Alcor 差异见 [三方对齐说明](docs/03_alcor_generation_alignment.md)，开发前必须核对 [现有能力复用矩阵](docs/01_existing_capability_reuse_matrix.md)，实施顺序见 [开发实施计划](docs/06_development_plan.md)。

## 后续开发入口

- [MVP 功能方案](docs/04_mvp_functional_spec.md)：明确要建设的功能、模块、接口、数据模型和安全边界；
- [逐步实施清单](docs/05_step_by_step_implementation.md)：从 DF-000 到 DF-025，每项都有前置条件、产出和完成判定；
- [MVP 验收方案](docs/07_acceptance_test_plan.md)：阶段 Gate、验收用例、性能/稳定性标准和最终签收条件。

后续按 `DF-000 → DF-001 → ... → DF-025` 执行。`ALCOR-001` 等新版 Alcor 实际 OpenAPI 可用后再开始，不阻塞设备农场 MVP 独立完成。

本地构建和测试入口见 [开发说明](docs/development.md)。

设备接口以 [OpenAPI 契约](openapi/device-farm-v1.yaml) 为准。`/api/v1/device-*` 使用平台服务 Token，`/internal/v1` 使用独立 Agent Token；两类 Token 必须不同，空值不会放行受保护接口。

配置 PostgreSQL URL 后，Server 已可提供 Image、Host、Pool 和 Device 管理 API；当前设备实例由 Mock Provider 支撑，Reservation/Scheduler 从 DF-009 开始接入。
