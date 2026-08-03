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

设备接口以 [OpenAPI 契约](openapi/device-farm-v1.yaml) 为准。`/api/v1/device-*` 使用平台服务 Token，`/internal/v1` 使用独立 Agent Token；两类 Token 必须不同，空值不会放行受保护接口。current/previous 双 Token 支持无中断轮换，旧 Token 从 previous 配置移除后立即失效。管理设备操作会保存操作者、request ID、原因和动作，日志、健康事件及审计写入前会脱敏；规则见 [安全与审计](docs/security_and_audit.md)。

配置 PostgreSQL URL 后，Server 已可提供 Image、Host、Pool、Device 和 Reservation API；当前设备实例由 Mock Provider 支撑，Scheduler 会按设备池、能力、Host 状态、设备状态和并发上限完成原子分配并创建 Session。

Reservation 支持幂等续租、主动/强制释放和超时回收。Scheduler 周期、Reaper 周期与 grace period 可通过 `DEVICE_FARM_LEASE_*` 配置；释放或过期会原子关闭 Session、将 Device 送入 recycling 并写入设备审计事件。固定目标 Controller 随后创建持久化 rebuild Host Command，Agent 删除旧容器、网络和数据卷后重新创建；只有 ADB、Android boot 和 Appium 全部健康才回到 ready，失败则重试并最终隔离。

Reconciler 会核对 Host/Agent 心跳上报的 ADB、启动、Appium 健康和可插拔 STF 可见性；真实 Provider 操作仍只在 Host Agent 内发生。健康事件统一写入 PostgreSQL，连续失败达到阈值后自动隔离，隔离设备只能通过既有人工解除/重建流程恢复。周期、Host 超时和失败阈值使用 `DEVICE_FARM_RECONCILE_*` 配置。

Host Agent 内部协议已提供 heartbeat、command claim 和 completion。命令由 PostgreSQL lease token + attempt 防止重复或旧 Agent 回写；租约过期或 Agent 明确上报可重试失败后，按最大尝试次数安全重领，最终才转为 failed/timed_out。

`device-host-agent` 已是可运行进程：使用已注册 Host ID 和独立 Agent Token，周期发现本机 Provider 设备并心跳，长轮询领取命令，按并发上限执行 Mock Provider 操作；收到退出信号后先停止领取，再等待在途命令完成，超时未完成的命令由 Server lease recovery 接管。

Host Agent 可通过 `DEVICE_FARM_AGENT_PROVIDER=mock|docker` 选择 Provider。Docker 模式只允许在具备可读写 `/dev/kvm` 的 Linux Host 启动，并使用固定版本镜像、独立网络/数据卷、Docker 随机 ADB 端口和 CPU/内存/PID 限制；配置与 Linux 双设备验收入口见 [Docker Emulator Provider](docs/docker_emulator_provider.md)。

镜像 validation 和固定目标 Controller 已通过 Host Command 接入 Agent。管理端设置 `device_pool_images.min_ready/max_instances` 后，后台会自动登记并补齐 Emulator，不需要手工创建设备；默认两台时设置 `2/2`，以后扩容只改参数。详细边界与操作见 [固定目标模拟器池](docs/warm_pool_controller.md)。

STF 与 RethinkDB 的最小内网部署已固定为 DeviceFarmer/STF `3.7.9` 和 RethinkDB `2.4.2`。默认只绑定本机回环地址，RethinkDB、ADB server 和管理 Token 不暴露给浏览器；部署和真实验收见 [STF 单机内网部署](deploy/stf/README.md)。

Server 启用 `DEVICE_FARM_STF_ENABLED=true` 后，Scheduler 会在 Reservation 进入 active 前调用 STF claim；claim 失败会补偿数据库和设备状态。主动释放与过期回收必须先完成 STF release，失败时预约保持 active 并记录审计。`POST /api/v1/device-reservations/{id}/remote-sessions` 只向匹配的预约 owner 返回短时 STF remoteConnect ADB 地址，过期后由 Reaper 调用 remoteDisconnect；STF API Token 永不进入响应。配置、流程和限制见 [STF Adapter 与预约编排](docs/stf_adapter.md)。

Server 已提供 `/healthz`、数据库感知的 `/readyz` 和 Prometheus `/metrics`。Linux systemd、Docker Compose、备份、升级、告警、运维和回滚入口见 [Server 部署](deploy/server/README.md)、[可观测性](docs/observability.md)、[运维手册](docs/operations_runbook.md) 和 [回滚方案](docs/rollback.md)。

新版 Alcor 接口尚未完成时，可直接使用 [Adapter 契约包](docs/alcor_adapter_contract.md) 独立联调。包内包含冻结的 Device Farm OpenAPI、RunAttempt 示例客户端、可运行 Mock Server、错误映射和契约测试；Mock 不需要 PostgreSQL、Docker 或真实设备。真实 Alcor 接口到位后只需在 Worker 中实现同一调用边界，不改变设备农场内部架构。
