# DF-021 实施与验收证据

## 当前结论

故障恢复、释放后自动重建回池和数据隔离编排已完成本地实现。当前机器没有 Linux KVM、Docker Emulator、STF 和远程 Appium，无法验证真实数据卷、App 数据和 50 次稳定循环，因此 DF-021 状态为 `blocked`，不能标记 `completed`。

## 已完成交付

- Agent create 会创建、启动并等待 ADB、Android boot、Appium 全部健康，失败时清理部分资源；
- Agent rebuild 使用幂等 Delete + 全新 Create，不复用上一 Reservation 的数据卷；
- Agent 明确上报 retryable 后，Host Command 在最大次数内回到 pending，而不是一次失败即终止；
- Reservation released/expired/force_released 后，固定目标 Controller 按 Device + Reservation 创建唯一 rebuild 命令；
- Server 重启后可从数据库中的 create/rebuild command 结果继续恢复，不依赖进程内状态；
- rebuild 结果必须包含 serial、ADB/Appium Endpoint、Appium UDID 和完整健康快照，缺失时隔离；
- rebuild 成功才从 recycling 回 ready；最终失败或超时进入 quarantined/unhealthy 并写健康事件；
- Server Reconciler 不调用真实 Provider，不需要 Docker Socket；它使用 Agent heartbeat、PostgreSQL 和 STF 可见性收敛状态；
- Docker Provider 测试证明 rebuild 删除带上一任务标记的旧数据卷并创建干净卷；
- 补偿矩阵、数据隔离规则和人工介入条件见 `docs/failure_recovery_and_data_isolation.md`。

## 本地验收结果

```text
PASS Agent boot timeout -> DEVICE_BOOT_TIMEOUT + retryable + 清理
PASS Agent Appium unhealthy -> APPIUM_UNHEALTHY + retryable + 清理
PASS retryable Host Command 在最大次数内重新 pending 并可再次成功
PASS Server 重启后从 succeeded create 结果恢复 Device ready
PASS released Device 只创建一条 rebuild 命令
PASS rebuild 完整健康后 recycling -> ready
PASS rebuild 最终超时后 recycling -> quarantined 并写健康事件
PASS Server 无 Provider 访问时依据 Agent unhealthy 结果隔离设备
PASS Docker rebuild 不复用带上一任务标记的数据卷
PASS 原有 Scheduler/Reaper/STF/Appium/Agent/Warm Pool 测试不回归
```

## 真实环境验收

1. 在 Linux KVM Emulator 安装测试 App，写入 App data、cache 和约定外部存储标记；
2. release 后确认设备进入 recycling，旧容器、网络和数据卷被删除；
3. 等待 rebuild 完成并回 ready，再创建下一 Reservation；
4. 检查上一任务安装包、App data、cache 和文件均不可见，检出率为 0；
5. 分别注入 Agent 离线、Server 重启、STF 超时、Appium 不健康、boot timeout、命令超时和 Docker volume 删除失败；
6. 确认每类故障进入 released/expired/failed/quarantined 中的明确状态，无永久 reserved/busy/recycling 悬挂；
7. 连续运行至少 50 次申请、执行、释放、重建循环，确认零双占、零永久悬挂、Docker 资源无持续增长。

## 阻塞解除条件

在 Linux KVM 服务器完成上述故障注入、数据检出率 0 和至少 50 次稳定循环后，将 DF-021 改为 `completed`。Mock 数据卷标记测试不能替代真实 Android 文件和 Docker volume 验收。
