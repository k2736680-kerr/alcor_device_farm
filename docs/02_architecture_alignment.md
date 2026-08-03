# 设备农场方案对齐表

## 1. 使用目的

本表把 [设备农场方案](reference/app_evaluation_device_farm_design.md) 中仍有效的设备域能力映射到当前 `alcor_device_farm` 工作区。Alcor 平台对象和接入边界同时受 [新版 Alcor 方案](reference/alcor_next_generation_plan.txt) 约束，冲突裁决见 [三方对齐说明](03_alcor_generation_alignment.md)。

当前单独建仓是因为新版 Alcor 明确把 Device Farm 放在第六阶段，相关 Adapter 尚未开发。设备域可以先行验证，但不得复制新版 Alcor 的评估业务。

## 2. 组件逐项对齐

| 组件 | 当前工作区处理 | 新版 Alcor 接入方式 | 禁止跑偏 |
|---|---|---|---|
| Eval Console 设备入口 | 当前不开发 | 新版 Alcor 第六阶段在 `eval_console` 增加 | 不创建独立 Console，不沿用旧 `alcor_console` 假设 |
| Case、Dataset、Target、Config | 当前不开发 | 由新版 `/api/v1` 和 PostgreSQL/ClickHouse 管理 | 不建临时替代业务表和 API |
| Run、RunAttempt、Result、Artifact | 当前不开发正式业务模型 | 由独立 Worker、PostgreSQL、ClickHouse、Supabase Storage 管理 | DaFit 报告只作为联调产物 |
| Device Farm Adapter | 提供北向 OpenAPI 和 Mock | 由新版 Worker 在第六阶段实现 | 不直接依赖旧 `eval_tasks` 或共享数据库 |
| Device Scheduler | 当前新增并独立测试 | Adapter 通过预约 API 使用 | 不混入 Run 队列、用例执行和评分 |
| Reconciler、Reaper | 当前新增并独立测试 | 设备农场内部能力 | 不以 STF 数据替代设备农场 PostgreSQL 真相 |
| Host Agent | 当前新增 | 只调用 `/internal/v1` | 不向 Agent 暴露业务数据库、钉钉身份或 Target 密钥 |
| Docker Emulator Provider | 当前新增 | 由设备农场调度 | 不把 Docker Socket 暴露给 Alcor/浏览器 |
| USB 真机 Provider | 只保留统一接口和扩展点 | 后续新增 `USBPhysicalDeviceProvider` | 不改 Scheduler、Reservation、STF、Appium 上层模型 |
| STF + RethinkDB | Adapter 和部署配置 | 继续作为远控/可见性工具 | 不作为预约和占用真相源 |
| Appium 2 + UiAutomator2 | Adapter 管理 Endpoint 和健康 | Worker 获得设备后使用 Appium 执行器 | 不重写 WebDriver 协议 |
| DaFit 自动化执行器 | 通过 Harness 做首个真实联调 | 为未来 Android Executor 提供成熟实现和验证样本 | 不复制页面、动作、断言、证据和报告形成双实现 |
| 设备域 PostgreSQL | 保存 Host、Device、Pool、Reservation、健康状态 | Alcor 通过 `owner_type=run_attempt`、`owner_id` 关联；人工/DaFit 使用受控类型 | 不保存 Run/Result，不与 Alcor 跨库建外键 |

## 3. 当前必须实现的设备域

- Device Image、Host、Host Command、Pool、Device、Reservation；
- Host Agent 注册、心跳、命令领取、回报和排空；
- Docker Emulator 生命周期和 USB Provider 扩展接口；
- Scheduler、租约、续租、释放、数据库并发约束；
- Reconciler、Reaper、健康事件、隔离和重建；
- STF inventory/claim/release/remoteConnect Adapter；
- Appium Endpoint、端口和健康状态 Adapter；
- `/api/v1/device-*` 与 `/internal/v1` 契约；
- UUID/ULID Owner ID、幂等键和统一错误响应；
- `X-Eval-Run-Id`、`X-Eval-Attempt-Id`、`traceparent` 透传；
- DaFit 端到端 Harness。

## 4. 当前目录职责

| 当前工作区 | 当前职责 | 第六阶段接入 |
|---|---|---|
| `cmd/device-farm-server` | 设备农场 Server 二进制入口 | 新版 Worker 的 Device Farm Adapter 调用 |
| `cmd/device-host-agent` | 宿主机 Agent 二进制入口 | 只和设备农场 `/internal/v1` 通信 |
| `internal/api/service/repository` | 设备北向 API、业务编排和设备域持久化 | 对 Alcor 只暴露 OpenAPI |
| `internal/scheduler/reconciler/reaper` | 设备分配、租约和状态收敛 | 对 Alcor 保持内部不可见 |
| `internal/providers` | Docker Emulator、Mock、USB 扩展 | 上层统一 Device 模型不变 |
| `internal/adapters/stf` | STF API 封装 | 由设备农场内部调用 |
| `internal/adapters/appium` | Endpoint、端口和健康管理 | Endpoint 随 Reservation 返回 Worker |
| `migrations` | 设备域表和约束 | 不并入新版 Run/Case migration，不跨库外键 |
| `deploy` | STF、Agent、模拟器和设备服务部署 | 独立部署细节对 Alcor Adapter 不可见 |
| `harness/dafit` | Alcor 接入前真实联调 | 不进入 Eval Console 或 Run 业务模型 |

新版方案未确定设备农场最终是否与 Alcor 合仓，因此当前目录不预设迁入旧 `eval_server/internal/devicefarm`，也不预设永远不能合仓。第六阶段只以稳定 API 契约作为必需边界。

## 5. 当前明确延期或归属 Alcor 的业务域

- 钉钉用户、权限、操作人筛选和审计；
- Protocol Template、Case、Dataset 和 ClickHouse 明细；
- Target、Config、YAML Secret 引用；
- Run、RunAttempt、Worker 业务队列和执行快照；
- RunResult、用例级结果、评分、门禁、对比和复核；
- Supabase Storage Artifact、LLM 报告和 HyperDX/WeData 观测；
- Eval Console 页面和 CI/发布门禁入口。

这些能力由新版 Alcor 实施，设备农场只提供设备资源，不建立等待同步的替代对象。

## 6. 接入不变式

1. Eval Console 和 `/api/v1` 是评估业务入口；设备 UI 未来也进入 Eval Console；
2. Alcor 保存 Run/RunAttempt/Result/Artifact 业务真相，设备农场保存 Device/Reservation 技术资源真相；
3. STF 只负责设备可见性和远程控制；Docker 只负责运行载体；
4. 上层只依赖统一 Device，模拟器和真机使用相同的池、预约、Session 和 Appium 链路；
5. Worker 通过 Device Farm Adapter 和 API 申请设备，不共享数据库；
6. Alcor Reservation Owner 使用 RunAttempt UUID/ULID；人工和 DaFit 可使用 `manual/test_run`，但不得固化旧整数 Task ID；
7. 业务报告和测试结果由 Alcor 写入 Supabase Storage/ClickHouse，设备农场不留副本；
8. DaFit 是成熟执行能力的复用来源和首个联调负载，不是设备农场业务模块。

## 7. 文档优先级

1. 设备内部生命周期、调度、STF、Appium、Host Agent 和真机扩展：设备农场方案优先；
2. Alcor 对象、API、ID、存储、前端、队列和集成边界：新版 Alcor 方案优先；
3. 两份方案未覆盖的实现细节：本对齐表、三方对齐说明和 ADR；
4. 当前旧版 Alcor master 只作为历史事实和迁移来源，不覆盖新版方案；
5. 代码实现优先级最低，不能用“已经写了”改变方案。
