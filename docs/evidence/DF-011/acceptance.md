# DF-011 验收证据

## 交付内容

- `POST /internal/v1/devices/{id}/health-events` 已实现，只允许 Agent Token；
- 健康事件、Device 健康、连续失败次数和自动隔离在一个 PostgreSQL 事务内更新；
- Reconciler 使用现有 Mock Provider 核对 Provider 存在性、ADB、启动完成和 Appium 健康；
- STF 尚未到 DF-017，当前通过小型 `Visibility` 接口预留对接，并使用 Mock 验证不可见场景，没有重写 STF；
- Host 心跳超时通过 Host 领域状态机变为 offline，Scheduler 已有查询会自动排除该 Host；
- warning/error/critical 事件形成明确 health reason，连续失败达到配置阈值后通过 Device 状态机进入 quarantined；
- quarantined Device 不会被 Reconciler 自动恢复；人工解除隔离复用已有 Device API，并把失败计数归零；
- Server 与现有 Management、Reservation、Scheduler、Reaper 共用同一个 Mock Provider 实例，避免出现两份 Provider 内存真相；
- Reconciler 周期、Host 超时和失败阈值已加入 YAML 与环境变量配置。

## 状态收敛验收

真实 PostgreSQL 17.10 + Mock Provider 测试结果：

- `TestProviderMissingQuarantinesReadyDatabaseDevice`：数据库为 ready、Provider 不存在时，设备立即变为 unhealthy + quarantined，并记录 `provider_device_missing`；
- `TestStaleHostGoesOfflineAndDeviceStopsScheduling`：心跳超过阈值后 Host 变为 offline，设备不再出现在可调度集合；
- `TestRepeatedAppiumFailureQuarantinesAndManualRecoveryResetsCounter`：Appium 连续失败达到阈值后隔离；人工解除后进入 provisioning + unknown，失败计数清零；
- `TestSTFInvisibleConvergesToQuarantineButHealthyDoesNotAutoRecover`：STF 连续不可见后隔离；随后可见也不会绕过人工恢复；
- `TestHealthReportValidationAndMissingDevice`：非法事件和不存在设备分别返回明确错误，不写脏数据。

## API 与权限验收

`TestAgentHealthEventAPIQuarantinesAfterThreshold` 验证：

- 无 Token 调 internal health API：401；
- Service Token 调 internal health API：403；
- Agent Token 连续上报 3 次 Appium error：均返回 201；
- 最终 Device 为 quarantined + unhealthy，`consecutive_failures=3`；
- 外部上报不能冒充保留的 `reconciler` source。

## 架构复用检查

- Device 状态仍由 DF-005 状态机控制；
- Provider 仍使用 DF-007 接口和 Mock；
- 设备查询、隔离/解除隔离仍使用 DF-008 管理能力；
- Scheduler 排除 unhealthy/quarantined/offline Host 的规则仍使用 DF-009 实现；
- `device_health_events`、`devices.consecutive_failures` 和 `device_hosts.last_heartbeat_at` 均复用 DF-004 表，没有新增重复表；
- 没有创建 Alcor Run、Result、Artifact 或前端对象。

## 验收命令与结果

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-migrations.ps1 -RunRepositoryTests
```

关键结果：

```text
PASS TestProviderMissingQuarantinesReadyDatabaseDevice
PASS TestStaleHostGoesOfflineAndDeviceStopsScheduling
PASS TestRepeatedAppiumFailureQuarantinesAndManualRecoveryResetsCounter
PASS TestSTFInvisibleConvergesToQuarantineButHealthyDoesNotAutoRecover
PASS TestHealthReportValidationAndMissingDevice
PASS TestAgentHealthEventAPIQuarantinesAfterThreshold
PASS internal/reconcile
PASS internal/api
```

`scripts/dev.ps1 -Task check` 负责全量格式、静态检查、测试与两个程序构建。临时 PostgreSQL 在测试后已停止并删除数据目录。

## 验收结论

DF-011 已满足健康事件、Provider/Host/STF 状态核对、连续失败隔离和人工恢复的完成条件，预约正确性 Gate G2 完成，可以进入 DF-012 Agent 内部协议和 Host Command。
