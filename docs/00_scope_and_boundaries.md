# 项目范围与系统边界

## 1. 项目定位

`alcor_device_farm` 是设备基础设施子系统，不是另一套评估业务平台。

当前阶段在新版 Alcor 的 Device Farm Adapter 尚未完成时，使用独立仓库开发和验证设备域。新版 Alcor 负责评估业务和 RunAttempt 真相；本项目负责设备、宿主机、池、预约和技术连接真相。STF 只负责远控和设备可见性，Docker 只负责运行载体。

新版方案将 Device Farm 排在第六阶段，并明确由独立 Worker 通过 Device Farm Adapter 使用。当前先固化可测试的北向 API；接入时由 Adapter 以 RunAttempt 身份申请、续租和释放设备，不直接读取或修改对方数据库。新版方案尚未确定设备域最终是否合仓，因此当前不得假设必须迁入旧 Alcor 目录。

## 2. 设备农场负责

- Device Image、Device Host、Device、Device Pool；
- Host Agent 注册、心跳、命令领取和结果上报；
- Docker Emulator 与 USB Android Provider；
- 设备发现、准备、健康检查、重启、回收和移除；
- STF inventory、claim、release 和 remoteConnect；
- 设备预约、租约、并发锁和过期回收；
- 设备状态机、健康事件、隔离和重建；
- Appium Endpoint 的启动、端口隔离和健康状态；
- 面向外部任务系统的幂等北向 API。

## 3. 新版 Alcor 负责

- 钉钉用户、权限、审计和 Eval Console；
- Protocol Template、Case、Dataset 及不可变版本；
- Target、Config 及其版本；
- Run、RunAttempt、独立 Worker 和 PostgreSQL 租约队列；
- 用例执行、结果、评分、门禁、报告和历史对比；
- ClickHouse 中的大数据集明细、用例级结果和指标摘要；
- Supabase Storage 中的媒体、报告、日志和制品；
- 通过 Device Farm Adapter 将 RunAttempt ID 作为设备预约 Owner ID。

## 4. DaFit 项目当前负责

`dafit_auto_platform` 是首个真实执行负载，用来验证设备农场分配的设备和 Appium Endpoint 可以完成真实自动化。当前不复制其页面、组件、场景、断言和报告代码。

联调时只增加薄适配：

- Farm 模式下必须使用外部指定的 `ANDROID_UDID`；
- 使用外部指定的 `APPIUM_SERVER`；
- 禁止自动选择第一台设备；
- 禁止自行启动远程 Emulator 或 Appium；
- 每次运行写入外部指定的独立报告目录；
- 原本地单机运行行为保持不变。

## 5. 数据所有权

当前独立开发环境只保存设备域数据，并使用 PostgreSQL 验证事务、租约和唯一约束。不创建 Alcor 的 Case、Dataset、Run、RunAttempt、Result 或 Artifact 业务表。

新版 Alcor 的 PostgreSQL 保存业务对象、RunAttempt 和 Artifact 索引；设备农场 PostgreSQL 保存 Host、Device、Pool、Reservation 和技术健康状态。两个系统只通过 API 和外部 ID 关联，不跨库建外键，也不双写对方领域数据。

设备预约使用通用外部关联字段：

```text
owner_type = run_attempt | manual | test_run
owner_id
idempotency_key
```

当前 DaFit 联调使用 `owner_type=test_run` 和测试 UUID；人工调试使用 `manual`；未来 Alcor 自动执行使用 `run_attempt` 和 RunAttempt UUID/ULID。设备农场不保存旧版整数 Eval Task ID，也不建立跨系统数据库外键。

北向设备接口沿用确定方案命名：`/api/v1/device-images`、`/api/v1/device-hosts`、`/api/v1/device-pools`、`/api/v1/devices` 和 `/api/v1/device-reservations`；Agent 内部接口使用 `/internal/v1`。不得另外发明一套不兼容路径。

DaFit 生成的 HTML/JSON、截图和日志只在联调阶段由 Harness 返回路径；正式接入后由新版 Alcor Worker 通过 ArtifactStore 上传 Supabase Storage，设备农场不保存业务报告。

## 6. 不允许形成的重复能力

- 不在本项目创建旧版 `eval_tasks/eval_results`，也不创建新版 `runs/run_attempts/run_results` 或评分规则；
- 不在 STF RethinkDB 保存业务预约真相；
- 不允许浏览器直接持有 STF Token；
- 不允许 Alcor 或服务端直接暴露 Docker Socket；
- 不使用进程内锁代替 PostgreSQL 并发约束；
- 不把 DaFit 业务代码变成设备农场的一部分。

现有能力的具体归属和允许新建范围以 [现有能力复用矩阵](01_existing_capability_reuse_matrix.md) 为准。
