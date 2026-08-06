# 设备农场 MVP 验收报告

## 结论

E0 控制面、并发、租约、契约和 migration 门禁全部通过；E1/E2 的 Linux KVM、Android 16 Emulator、ADB、Appium、STF、DaFit、故障恢复、数据隔离、50 次稳定性、安全、告警和回滚均真实通过。设备域 G0～G6 结论为 `PASS`，DF-024 可以完成。

Device Farm Console 的浏览器部署、受控 STF 看屏和 Web 安全属于 G7/DF-028，单独保持 `IN_PROGRESS`；新版 Alcor 真实接口联调属于 ALCOR-001，保持外部等待，不影响独立设备农场 G0～G6。

状态定义：

- `PASS-E0`：Windows + Mock + 真实临时 PostgreSQL 自动化通过；
- `PASS-E1`：Linux KVM、Docker Emulator、ADB 或 Host Agent 真实通过；
- `PASS-E2`：STF、Appium、DaFit 或全链路故障场景真实通过；
- `PASS-REAL`：真实安全、稳定性、告警、备份或回滚通过；
- `PASS-MOCK`：不依赖真实设备的 Adapter 客户端和错误契约通过；
- `P2-NOT-RUN`：正式验收不要求、资源允许时再执行的扩展项；
- `PENDING-G7`：属于 DF-028 的浏览器验收，不属于 DF-024 设备域签收范围。

## Gate

| Gate | 状态 | 证据 |
|---|---|---|
| G0 文档基线 | PASS | DF-000 evidence |
| G1 Mock 控制面 | PASS-E0 | DF-001～DF-008 evidence、local gate |
| G2 预约正确性 | PASS-E0 | 并发 Scheduler/Reaper/Repository 集成测试 |
| G3 Docker 设备 | PASS-E1 | DF-014～DF-016 真实 Android 16、Appium、自动补池证据 |
| G4 STF/Appium | PASS-E2 | DF-017～DF-018 真实 STF inventory/claim/release/入口证据 |
| G5 DaFit 闭环 | PASS-E2 | DF-019～DF-021 成功/失败/中断/Reaper/数据隔离/50 次循环证据 |
| G6 可交付 | PASS-REAL | DF-022 安全、DF-023 告警与回滚、DF-024 全量门禁、DF-025 契约包 |
| G7 Web 可用 | IN_PROGRESS | DF-026/DF-027 E0 已完成，DF-028 真实浏览器验收进行中 |

## 用例结果

| 编号 | 状态 | 自动化或证据 |
|---|---|---|
| AT-API-001 | PASS-E0 | auth/API integration |
| AT-API-002 | PASS-REAL | DF-022/DF-024 Service/Agent 路径隔离，真实 403 |
| AT-API-003 | PASS-REAL | DF-022 actor/request ID/reason 原子审计 |
| AT-API-004 | PASS-E0 | Reservation 幂等集成测试 |
| AT-API-005 | PASS-E0 | management/reservation 参数测试 |
| AT-API-006 | PASS-E0 | OpenAPI contract test |
| AT-DB-001 | PASS-E0 | migration up/down/up |
| AT-DB-002 | PASS-E0 | domain state machine tests |
| AT-DB-003 | PASS-E0 | quarantined 不可调度集成测试 |
| AT-DB-004 | PASS-E0 | active reservation 唯一约束 |
| AT-DB-005 | PASS-E0 | Repository rollback test |
| AT-DB-006 | PASS-REAL | DF-024 PostgreSQL 停止时请求 500；恢复后同一幂等键仅创建 1 条预约 |
| AT-SCH-001 | PASS-E0 | 100 并发预约竞争两设备，0 双占 |
| AT-SCH-002 | PASS-E0 | capability matching tests |
| AT-SCH-003 | PASS-REAL | DF-023 API 36/ABI 不匹配预约保持 pending 并触发 backlog 告警 |
| AT-SCH-004 | PASS-E0 | extension 集成测试 |
| AT-SCH-005 | PASS-E0 | 终态/过期续租拒绝 |
| AT-SCH-006 | PASS-E0 | concurrent release |
| AT-SCH-007 | PASS-E0 | concurrent Scheduler |
| AT-SCH-008 | PASS-E0 | concurrent Reaper |
| AT-SCH-009 | PASS-E2 | DF-021 真实 warm 设备 50 次预约循环 |
| AT-SCH-010 | PASS-E2 | DF-020 强制终止后 Reaper 真实过期回收 |
| AT-AGT-001 | PASS-E1 | DF-014～DF-016/DF-024 真实 Agent 心跳、容量与 Host online |
| AT-AGT-002 | PASS-E1 | DF-021/DF-023 停止 Agent 后 Host offline/告警并恢复 |
| AT-AGT-003 | PASS-E0 | lease token/attempt 防旧完成 |
| AT-AGT-004 | PASS-E1 | DF-021 Agent 执行中断、租约重领和第 2 次成功 |
| AT-EMU-001 | PASS-E1 | Android 16/API 36、ADB online、boot completed |
| AT-EMU-002 | PASS-E1 | ADB、boot、Appium healthy 后 Device ready |
| AT-EMU-003 | PASS-E1 | 无 KVM 明确 `KVM_UNAVAILABLE`，真实环境 `/dev/kvm` 可读写 |
| AT-EMU-004 | PASS-E2 | DF-021 50 次重建后 App/内部与外部存储标记检出率 0 |
| AT-EMU-005 | PASS-E1 | DF-016 per-image runtime selection 和 rebuild 保持 Image |
| AT-EMU-006 | PASS-E2 | DF-021 删除/重建后容器、网络、卷无增长 |
| AT-EMU-007 | PASS-E1 | `min_ready=1/max_instances=1` 自动补池且第二预约不突破容量 |
| AT-EMU-008 | P2-NOT-RUN | ADR-0008 单 Emulator 基线；多设备端口隔离由自动化契约覆盖 |
| AT-STF-001 | PASS-E2 | DF-017/DF-024 STF serial 唯一、present、ready |
| AT-STF-002 | PASS-E2 | DF-018/DF-020 claim 后才 active |
| AT-STF-003 | PASS-E2 | DF-021 STF timeout 分类和补偿 |
| AT-STF-004 | PASS-E2 | DF-020 release 403 inventory 复核与 Reaper 重试 |
| AT-STF-005 | PASS-E2 | DF-018 短时 remoteConnect，不返回管理 Token |
| AT-STF-006 | PASS-E2 | DF-018 owner 越权 403 |
| AT-APP-001 | PASS-E2 | DF-015/DF-019 UiAutomator2 Session 创建删除和 Appium 3.5.2 |
| AT-APP-002 | PASS-E2 | DF-021/DF-023 Appium unhealthy 隔离、告警和恢复 |
| AT-APP-003 | P2-NOT-RUN | ADR-0008 单 Emulator 基线；双 Session 仅多设备扩展环境执行 |
| AT-DFT-001 | PASS-E0 | 2026-08-06 collect-only=158 |
| AT-DFT-002 | PASS-E0 | DF-019 Farm 参数快速失败，不回退第一台设备 |
| AT-DFT-003 | PASS-E2 | DF-019/DF-020 真实成功冒烟、报告和释放 |
| AT-DFT-004 | PASS-E2 | DF-020 故意失败报告保留且预约释放 |
| AT-DFT-005 | PASS-E2 | DF-020 Harness/Python 强杀后 Reaper 回收 |
| AT-DFT-006 | PASS-E2 | DF-021 连续 50 次跨任务数据检出率 0 |
| AT-DFT-007 | P2-NOT-RUN | 按 ADR-0008 修正为两台真实设备可选扩展；单设备容量上限仍为 P0 |
| AT-REL-001 | PASS-E2 | DF-021 active Reservation 期间 Server 重启后状态恢复 |
| AT-REL-002 | PASS-E2 | DF-021 Agent 离线后停止分配并隔离/恢复 |
| AT-REL-003 | PASS-E2 | DF-021 boot timeout 命令最终失败且设备不 ready |
| AT-REL-004 | PASS-REAL | DF-021 STF/Appium/boot 故障和 DF-023 PostgreSQL 故障均明确收敛 |
| AT-SEC-001 | PASS-REAL | DF-022 Token/canary 在日志、响应、数据库 dump 精确命中 0 |
| AT-SEC-002 | PASS-E0 | reason 强制和敏感 reason 400 |
| AT-SEC-003 | PASS-REAL | DF-022 current/previous 轮换，旧 Agent Token 401 |
| AT-SEC-004 | PASS-REAL | Server 无 Docker Socket、非 root、只读、CapDrop=ALL |
| AT-REL-005 | PASS-E2 | DF-021 连续 50 次，0 双占、0 永久悬挂、资源 1/1/1 |
| AT-ALC-001 | PASS-MOCK | RunAttempt UUID/ULID 创建 pending 预约 |
| AT-ALC-002 | PASS-MOCK | `DEVICE_CAPACITY_UNAVAILABLE` 映射 `retry_capacity` |
| AT-ALC-003 | PASS-MOCK | 不可重试 `KVM_UNAVAILABLE` 映射 `infra_failed` |
| AT-ALC-004 | PASS-MOCK | active release 幂等重放；等待超时映射容量重试 |
| AT-ALC-005 | PASS-MOCK | 冻结 OpenAPI 类型化示例客户端通过完整 Mock 生命周期 |
| AT-WEB-001～012 | PENDING-G7 | DF-026/DF-027 E0 已通过；真实登录、看屏、安全、重启和回滚由 DF-028 验收 |

## 非功能结果

| 指标 | 结果 |
|---|---|
| 双占 | PASS-E0：100 并发、两设备、0 双占；真实单设备第二预约不突破容量 |
| 普通查询 20 RPS p95 | PASS-E0：100 次请求 p95=739.3µs，标准 ≤300ms |
| warm 预约 10 秒 | PASS-E2：DF-021 真实 50 次循环满足 |
| 过期回收 60 秒 | PASS-E2：DF-020 强杀后 Reaper 实际回收 |
| 120 秒状态收敛 | PASS-E2/REAL：Server、Agent、Appium、数据库故障均恢复 |
| 8 小时/50 次稳定性 | PASS-E2：采用“至少 50 次”条件，连续 50/50 通过 |
| 数据隔离检出率 0 | PASS-E2：App 数据、缓存、内部/外部测试标记均不可见 |
| 密钥泄露 0 | PASS-REAL：Server/Agent 日志、API、审计、数据库 dump 精确命中 0 |
| migration up/down/up | PASS-E0；DF-023 另完成 fresh migration 和重复执行保护 |
| 备份和回滚 | PASS-REAL：custom-format 备份、SHA 校验、隔离恢复、旧版本 recovery9 启动 |
| 临时资源残留 | PASS-REAL：开放预约/命令 0，受管资源 1/1/1，DF-022～024 临时容器和监控进程 0 |

## 签收结论

- 设备域技术验收：`PASS`（G0～G6）；
- 自动化与真实环境证据：完整，入口见 DF-014～DF-024 acceptance；
- 发布恢复点：DF-023 备份、恢复和 rollback 证据通过；
- 浏览器交付：`IN_PROGRESS`，待 DF-028 完成 G7；
- 新版 Alcor 联调：`WAITING_EXTERNAL`，待真实 Run/RunAttempt OpenAPI；
- DF-024 当前签收结论：`通过`。
