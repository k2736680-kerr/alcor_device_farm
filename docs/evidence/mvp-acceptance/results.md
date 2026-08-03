# 设备农场 MVP 验收报告

## 结论

E0 控制面、并发、租约、安全、指标和 migration 门禁已通过；E1/E2 真实设备链路不可用，因此 G3、G4、G5 和 G6 尚未通过，MVP 当前结论为 `BLOCKED`，不得签字为完成。

状态定义：

- `PASS-E0`：Windows + Mock + 真实临时 PostgreSQL 自动化通过；
- `PASS-STATIC`：契约、配置或部署安全静态门禁通过；
- `PASS-DAFIT`：DaFit 当前源码入口无设备检查通过；
- `BLOCKED-E1/E2`：必须在真实 Linux KVM 或完整联调环境验证；
- `WAIT-DF025`：由下一步 Adapter 契约包完成。

## Gate

| Gate | 状态 | 证据 |
|---|---|---|
| G0 文档基线 | PASS | DF-000 evidence |
| G1 Mock 控制面 | PASS-E0 | DF-001～DF-008 evidence、本次 local gate |
| G2 预约正确性 | PASS-E0 | 并发 Scheduler/Reaper/Repository 集成测试 |
| G3 Docker 设备 | BLOCKED-E1 | DF-014～DF-016 evidence |
| G4 STF/Appium | BLOCKED-E2 | DF-017～DF-018 evidence |
| G5 DaFit 闭环 | BLOCKED-E2 | DF-019～DF-021 evidence |
| G6 可交付 | BLOCKED | DF-022/023 已交付；DF-024 真实项阻塞；DF-025 待执行 |

## 用例结果

| 编号 | 状态 | 自动化或证据 |
|---|---|---|
| AT-API-001 | PASS-E0 | auth/API integration |
| AT-API-002 | PASS-E0 | Service/Agent 路径隔离测试 |
| AT-API-003 | PASS-E0 | correlation/server test |
| AT-API-004 | PASS-E0 | Reservation 幂等集成测试 |
| AT-API-005 | PASS-E0 | management/reservation 参数测试 |
| AT-API-006 | PASS-STATIC | OpenAPI contract test |
| AT-DB-001 | PASS-E0 | migration up/down/up |
| AT-DB-002 | PASS-E0 | domain state machine tests |
| AT-DB-003 | PASS-E0 | quarantined 不可调度集成测试 |
| AT-DB-004 | PASS-E0 | active reservation 唯一约束 |
| AT-DB-005 | PASS-E0 | Repository rollback test |
| AT-DB-006 | BLOCKED-E1 | 需真实网络断连和恢复 |
| AT-SCH-001 | PASS-E0 | 100 并发预约竞争两设备 |
| AT-SCH-002 | PASS-E0 | capability matching tests |
| AT-SCH-003 | PASS-E0 | mismatch pending test |
| AT-SCH-004 | PASS-E0 | extension 集成测试 |
| AT-SCH-005 | PASS-E0 | 终态/过期续租拒绝 |
| AT-SCH-006 | PASS-E0 | concurrent release |
| AT-SCH-007 | PASS-E0 | concurrent Scheduler |
| AT-SCH-008 | PASS-E0 | concurrent Reaper |
| AT-SCH-009 | PASS-E0 | warm Mock 预约及时激活 |
| AT-SCH-010 | PASS-E0 | grace period/Reaper tests |
| AT-AGT-001 | PASS-E0 | heartbeat 集成测试 |
| AT-AGT-002 | PASS-E0 | stale Host offline test |
| AT-AGT-003 | PASS-E0 | lease token/attempt 防旧完成 |
| AT-AGT-004 | PASS-E0 | lease recovery/retryable completion |
| AT-EMU-001 | BLOCKED-E1 | 需两台真实 Emulator |
| AT-EMU-002 | BLOCKED-E1 | 需真实 ADB/boot/Appium |
| AT-EMU-003 | BLOCKED-E1 | 需 Linux `/dev/kvm` 验证 |
| AT-EMU-004 | BLOCKED-E1 | 需真实 App/volume 数据隔离 |
| AT-EMU-005 | BLOCKED-E1 | 需真实 Docker 资源清理 |
| AT-EMU-006 | BLOCKED-E1 | Controller 逻辑已通过，真实自动补池待验 |
| AT-STF-001 | BLOCKED-E2 | 需真实 STF inventory |
| AT-STF-002 | BLOCKED-E2 | Mock claim 顺序通过，真实 STF 待验 |
| AT-STF-003 | BLOCKED-E2 | Mock 补偿通过，真实 STF 待验 |
| AT-STF-004 | BLOCKED-E2 | Mock release 审计通过，真实重试待验 |
| AT-STF-005 | BLOCKED-E2 | Adapter 脱敏通过，真实入口待验 |
| AT-STF-006 | BLOCKED-E2 | owner 校验通过，真实 STF 待验 |
| AT-APP-001 | BLOCKED-E2 | 需两台真实 Appium Session |
| AT-APP-002 | BLOCKED-E2 | 需真实 Appium unhealthy |
| AT-DFT-001 | PASS-DAFIT | 2026-08-03 collect-only=158 |
| AT-DFT-002 | PASS-DAFIT | DF-019 Farm 参数快速失败测试 |
| AT-DFT-003 | BLOCKED-E2 | 需真实成功冒烟 |
| AT-DFT-004 | BLOCKED-E2 | 需真实失败报告和释放 |
| AT-DFT-005 | BLOCKED-E2 | 需真实进程终止/Reaper |
| AT-DFT-006 | BLOCKED-E2 | 需跨任务 App 数据检出率 0 |
| AT-DFT-007 | BLOCKED-E2 | 需两设备 DaFit 并发 |
| AT-REL-001 | PASS-E0 | Server 重启后数据库结果续跑逻辑测试 |
| AT-REL-002 | PASS-E0 | Agent offline/Reconciler test |
| AT-REL-003 | PASS-E0 | boot timeout 编排测试；真实 E1 仍需复核 |
| AT-REL-004 | BLOCKED-E2 | Mock 分类通过，真实三依赖超时待验 |
| AT-SEC-001 | PASS-E0 | sensitive/log/DB canary tests |
| AT-SEC-002 | PASS-E0 | reason 强制测试 |
| AT-SEC-003 | PASS-E0 | current/previous 轮换和旧 Token 401 |
| AT-SEC-004 | PASS-STATIC | Server/Compose/systemd 无 Docker Socket；真实网络待复核 |
| AT-REL-005 | BLOCKED-E2 | 需 8 小时或至少 50 次真实循环 |
| AT-ALC-001 | WAIT-DF025 | Adapter Mock 契约包 |
| AT-ALC-002 | WAIT-DF025 | capacity retryable mapping |
| AT-ALC-003 | WAIT-DF025 | infra failure mapping |
| AT-ALC-004 | WAIT-DF025 | cancel/timeout release flow |
| AT-ALC-005 | WAIT-DF025 | generated/example client against Mock |

## 非功能结果

| 指标 | 结果 |
|---|---|
| 双占 | PASS-E0：100 并发、两设备、0 双占 |
| 普通查询 20 RPS p95 | PASS-E0：100 次请求实测 p95=739.3µs，标准 ≤300ms |
| warm 预约 10 秒 | PASS-E0 Mock；E1 待复核 |
| 过期回收 60 秒 | PASS-E0 虚拟时钟/真实 DB；E2 待复核 |
| 120 秒状态收敛 | PASS-E0 编排；E1/E2 待复核 |
| 8 小时/50 次稳定性 | BLOCKED-E2 |
| 数据隔离检出率 0 | BLOCKED-E1/E2 |
| 密钥泄露 0 | PASS-E0；真实集中日志待复核 |
| migration up/down/up | PASS-E0 |
| 真机扩展 | Provider/统一模型保留；DF-025 契约后复核 |

## 签字

- 技术负责人：待真实 E1/E2 验收后签字；
- 测试负责人：待所有 P0/P1 通过后签字；
- 发布负责人：待备份恢复和回滚演练后签字；
- 当前签收结论：`不通过（环境阻塞，非代码失败）`。
