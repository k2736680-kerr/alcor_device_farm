# 设备农场 MVP 验收方案

## 1. 验收原则

- `P0`：核心正确性和安全项，必须全部通过；
- `P1`：MVP 可运维和稳定性项，正式交付前必须通过；
- `P2`：增强项，可以记录为后续改进，但不得影响 P0/P1；
- 只有真实 Linux KVM、Docker Emulator、Appium 和 STF 环境通过后，才能宣称“真实设备农场 MVP 完成”；Mock 通过只代表控制面完成；
- 验收不能通过手工修改数据库制造成功状态。

## 2. 验收环境

### E0：本地 Mock 环境

- Windows 或 Linux 开发机；
- Device Farm Server；
- PostgreSQL；
- Mock Provider；
- 不要求 Docker Emulator、STF 和 Appium。

用途：接口、状态机、并发、幂等、Scheduler、Reaper、Reconciler 和故障测试。

### E1：Linux KVM 设备环境

- 支持 `/dev/kvm` 的 Linux Host；
- Docker Engine；
- Host Agent；
- 至少一个通过验证的 Android x86_64 镜像；
- 资源足以稳定运行两台 Emulator。

用途：真实创建、启动、ADB、boot、清理、重建和固定目标自动补齐。

### E2：完整联调环境

- E1 全部组件；
- STF + RethinkDB；
- Appium 2 + UiAutomator2；
- `dafit_auto_platform` Farm 适配分支；
- Device Farm Harness。

用途：远控、Appium 并发、DaFit 冒烟和全链路故障恢复。

## 3. 阶段验收门

| Gate | 对应任务 | 通过条件 |
|---|---|---|
| G0 文档基线 | DF-000 | 环境、依赖、端口和阻塞项清楚，无密钥泄露 |
| G1 Mock 控制面 | DF-001~DF-008 | E0 可管理全部核心资源，状态机和 migration 通过 |
| G2 预约正确性 | DF-009~DF-011 | E0 并发预约无双占，租约和状态可收敛 |
| G3 Docker 设备 | DF-012~DF-016 | E1 两台 Emulator 可创建、健康、补池和重建 |
| G4 STF/Appium | DF-017~DF-018 | E2 可远控、claim/release，Appium 端口隔离 |
| G5 DaFit 闭环 | DF-019~DF-021 | E2 冒烟成功/失败均可释放和清理 |
| G6 可交付 | DF-022~DF-025 | 安全、运维、回滚、全量验收和 Adapter 契约齐全 |

未通过前一 Gate，不进入下一阶段的真实环境部署。

## 4. 功能验收用例

### 4.1 API、认证和契约

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-API-001 | P0 | 无 Token 调用北向 API | 401，统一错误结构，不返回内部堆栈 |
| AT-API-002 | P0 | Agent Token 调用北向管理 API | 403，Agent 权限不能越界 |
| AT-API-003 | P0 | 请求携带 request/run/attempt/trace ID | 响应和日志可按关联 ID 查到同一链路 |
| AT-API-004 | P0 | 相同 Idempotency-Key 重复创建预约 | 返回同一 reservation，不重复占设备 |
| AT-API-005 | P0 | 非法 UUID/ULID、租期或能力 | 400 和稳定错误码，数据库无脏记录 |
| AT-API-006 | P1 | OpenAPI 示例和实际响应比对 | Schema 一致，无未记录字段 |

### 4.2 数据库和状态机

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-DB-001 | P0 | 空库执行 up/down/up | 全部成功且结构一致 |
| AT-DB-002 | P0 | 非法 Device 状态转换 | 领域层拒绝，状态不变并有错误事件 |
| AT-DB-003 | P0 | quarantined Device 参与调度 | 不得被选中 |
| AT-DB-004 | P0 | 同一 Device 插入两个 active reservation | 数据库唯一约束拒绝 |
| AT-DB-005 | P0 | Repository 事务中途失败 | 全部回滚，无半条 Session/Reservation |
| AT-DB-006 | P1 | 数据库短暂断开后恢复 | 服务恢复处理，未确认事务不被当作成功 |

### 4.3 Scheduler、Reservation 和租约

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-SCH-001 | P0 | 100 个并发预约竞争 2 台 ready 设备 | 任意时刻最多 2 个 active，无双占 |
| AT-SCH-002 | P0 | 要求 API/ABI/分辨率能力 | 只分配完全满足的设备 |
| AT-SCH-003 | P0 | 无满足能力设备 | pending 或明确容量失败，不错配设备 |
| AT-SCH-004 | P0 | 合法续租 | expires_at 延长且不超过最大租期 |
| AT-SCH-005 | P0 | 过期后续租 | 被拒绝，不复活旧预约 |
| AT-SCH-006 | P0 | 同时调用两次 release | 幂等成功，只执行一次底层释放 |
| AT-SCH-007 | P0 | 两个 Scheduler 实例同时领取 | 一个 pending reservation 只被处理一次 |
| AT-SCH-008 | P0 | 两个 Reaper 实例同时回收 | 只产生一次终态和清理命令 |
| AT-SCH-009 | P1 | ready warm 设备正常申请 | 10 秒内获得 active 或返回明确可重试错误 |
| AT-SCH-010 | P1 | 预约过期 | grace period 后 60 秒内关闭并进入清理 |

### 4.4 Host Agent 和 Docker Emulator

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-AGT-001 | P0 | Agent 注册和连续心跳 | Host online，容量和能力正确 |
| AT-AGT-002 | P0 | Agent 停止心跳 | 超时后 Host offline，不再接收新分配 |
| AT-AGT-003 | P0 | 命令重复领取/完成 | Provider 操作只执行一次，旧完成不能覆盖新结果 |
| AT-AGT-004 | P0 | Agent 执行中重启 | 命令最终可恢复、重领或明确失败，无永久 executing |
| AT-EMU-001 | P0 | 创建两台 Emulator | serial、ADB/Appium 端口、容器名互不冲突 |
| AT-EMU-002 | P0 | 启动健康检查 | ADB online、boot completed、Appium healthy 后才 ready |
| AT-EMU-003 | P0 | 无 `/dev/kvm` | 明确返回 KVM_UNAVAILABLE，不标记 ready |
| AT-EMU-004 | P0 | rebuild | 新实例不保留上一次 App 和测试文件 |
| AT-EMU-005 | P0 | 创建两个不同 Device Image | Host Command、Agent 校验和 Docker 容器分别使用各自 `docker_image`，rebuild 不串换版本 |
| AT-EMU-005 | P1 | 删除设备 | 容器、网络、端口、卷和数据库引用按策略清理 |
| AT-EMU-006 | P1 | `min_ready=2/max_instances=2` 且池为空 | 自动创建并加入两台；两个 Controller 并发不超建；第三个预约不突破上限 |

### 4.5 STF 和 Appium

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-STF-001 | P0 | STF inventory 同步 | serial 与 Device 唯一对应，STF 不覆盖业务状态 |
| AT-STF-002 | P0 | claim 成功 | Reservation 才可进入 active |
| AT-STF-003 | P0 | claim 失败 | 预约失败/重试并补偿，不返回连接信息 |
| AT-STF-004 | P0 | release 暂时失败 | 后台重试并审计，不能静默关闭底层占用 |
| AT-STF-005 | P0 | 请求远控入口 | 只返回短时入口，不返回管理 Token |
| AT-STF-006 | P0 | A 用户访问 B 预约远控 | 403，不能越权 |
| AT-APP-001 | P0 | 两台设备并发 Appium Session | 两个 Session 同时成功且 UDID 不串设备 |
| AT-APP-002 | P0 | Appium unhealthy | Device 不进入 ready 或被隔离 |

### 4.6 DaFit 闭环和数据隔离

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-DFT-001 | P0 | DaFit 当前 collect-only | 当前主线动态语言矩阵仍收集 158 个执行实例，与 Farm 改造前基线一致 |
| AT-DFT-002 | P0 | Farm 模式未传 UDID | 立即失败，不自动选择第一台设备 |
| AT-DFT-003 | P0 | 冒烟用例成功 | 报告生成、预约释放、设备进入 recycling/ready |
| AT-DFT-004 | P0 | 冒烟用例断言失败 | 失败报告保留，预约仍释放 |
| AT-DFT-005 | P0 | Harness 被强制终止 | Reaper 最终回收设备 |
| AT-DFT-006 | P0 | 下一预约检查上一任务数据 | App 数据、缓存和指定测试文件不可见 |
| AT-DFT-007 | P1 | 两个 DaFit 冒烟并发 | 各自使用独立设备和报告目录，不串数据 |

### 4.7 故障恢复和安全

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-REL-001 | P0 | Server 在 active reservation 时重启 | 重启后两分钟内恢复正确状态，不丢预约 |
| AT-REL-002 | P0 | Agent 离线 | 受影响设备停止分配并进入恢复/隔离 |
| AT-REL-003 | P0 | Emulator boot timeout | 命令失败，设备不 ready，按策略重建/隔离 |
| AT-REL-004 | P0 | PostgreSQL、STF、Appium 分别超时 | 错误分类准确，无永久悬挂资源 |
| AT-SEC-001 | P0 | 扫描响应、日志、审计和数据库普通字段 | 无 Token、密码、Cookie、STF 管理密钥 |
| AT-SEC-002 | P0 | 强制释放/隔离/重建无 reason | 请求被拒绝 |
| AT-SEC-003 | P0 | 过期或错误 Agent Token | 认证失败，不能上报伪造设备 |
| AT-SEC-004 | P0 | 从 Alcor/浏览器访问 Docker Socket | 网络和配置层均不可访问 |
| AT-REL-005 | P1 | 连续 8 小时或至少 50 次申请/执行/释放 | 零双占、零永久悬挂，资源无持续增长 |

### 4.8 新版 Alcor 契约准备

| 编号 | 优先级 | 场景 | 预期结果 |
|---|---|---|---|
| AT-ALC-001 | P0 | Mock RunAttempt 创建预约 | 使用 UUID/ULID Owner，不依赖旧 Eval Task |
| AT-ALC-002 | P0 | capacity unavailable | Adapter 可识别 retryable 并稍后重试 |
| AT-ALC-003 | P0 | Provider/STF/Appium 故障 | 可稳定映射为基础设施失败信息 |
| AT-ALC-004 | P0 | Attempt 取消/超时 | release 接口幂等，设备最终回收 |
| AT-ALC-005 | P1 | OpenAPI 生成客户端对 Mock Server 运行 | 申请、查询、续租、释放全部通过 |

## 5. 非功能指标

| 指标 | MVP 标准 |
|---|---|
| 双占 | 0 次 |
| 普通查询 API | 内网 E0、20 RPS 下 p95 ≤ 300 ms |
| 已有 warm 设备预约 | 正常依赖下 10 秒内 active |
| 过期回收 | grace period 后 60 秒内开始并完成可执行清理 |
| 状态收敛 | Server/Agent 重启或依赖恢复后 120 秒内 |
| 稳定性 | 8 小时或 50 次完整循环无永久悬挂和持续资源泄漏 |
| 数据隔离 | 上一任务测试数据检出率为 0 |
| 密钥泄露 | 响应、日志、报告和普通数据库字段中为 0 |
| migration | up/down/up 全通过 |
| 真机扩展 | Provider 接口编译级契约测试通过，不改上层模型 |

若验收机器资源不足导致性能指标不可比，必须记录机器规格和实测基线；不得删除正确性、安全性和双占标准。

## 6. 验收证据

每项测试至少保存：

- 测试编号、环境、时间、版本 commit；
- 执行命令或自动化入口；
- 关键请求/响应，必须脱敏；
- 数据库状态或事件时间线；
- 容器/设备/预约清理结果；
- 通过/失败结论和问题编号。

证据目录：

```text
docs/evidence/
├─ DF-xxx/
└─ mvp-acceptance/
   ├─ environment.md
   ├─ results.md
   ├─ logs-sanitized/
   └─ artifacts/
```

## 7. 最终签收条件

设备农场 MVP 只有满足以下条件才能签收：

1. 所有 P0、P1 用例通过；
2. DF-000~DF-025 全部 completed；
3. Linux KVM、两台 Emulator、STF、Appium 和 DaFit 冒烟真实通过；
4. 无双占、无永久悬挂、无跨任务数据残留、无密钥泄露；
5. OpenAPI、migration、部署、监控、故障处理和回滚文档齐全；
6. 新版 Alcor 团队可使用 Mock 契约包开发 Device Farm Adapter；
7. ALCOR-001 可以等待新版 Alcor 完成，不影响设备农场 MVP 独立签收。
