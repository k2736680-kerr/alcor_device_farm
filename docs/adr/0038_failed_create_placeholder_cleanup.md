# ADR-0038：失败创建占位自动补偿清理

## 状态

已确定；真实 Linux KVM 发布验收前不得标记 DF-070 completed。

## 背景

按需 Pool 的 `total_target=0` 依靠 pending Reservation 临时提高目标。一次 Android
Emulator create 命令耗尽重试后，Device 会进入 `quarantined/unhealthy`。ADR-0029
要求所有长期设备继续占用登记容量，避免健康抖动触发破坏性替换；现有 Warm Pool
因此也把这种从未创建成功的 provisioning 占位计入容量。

当 Pool `max_concurrency=1` 时，占位会永久满足登记容量，但不能被 Scheduler 使用，
后续 Reservation 一直 pending，系统也不会再创建 Emulator。

## 决策

1. 只把同时满足以下条件的记录认定为失败创建占位：create 命令为
   `failed/timed_out`、没有成功 create、序列号仍为 `pending-*`、没有 ADB/Appium
   Endpoint，并且从未被 Host 心跳观察到。
2. 有 pending/active 需求且可用或正在创建的实例不足时，Warm Pool 复用现有
   delete Host Command、Host Agent 和 Provider Delete 清理该占位；Server 不直接
   访问 Docker。
3. delete 完成前不创建替代实例；成功后 Device 只标记 `deleted`、保留审计历史，
   下一轮按原有容量和退避规则补建。
4. 任何曾经成功 create、出现过 Endpoint、被 Host 观察到，或后来因 STF、Agent、
   Appium、OOM、人工操作进入 quarantined 的设备都不属于占位，继续遵循 ADR-0029
   的非破坏恢复和人工删除规则。
5. 删除失败沿用现有重试、隔离、健康事件和告警，不通过盲目创建第三台设备掩盖
   Provider 资源是否仍存在的不确定性。

## 后果

- 创建命令永久失败不会再锁死 `target=0` 的按需 Pool；
- 长期设备的数据保留语义不变，健康隔离仍不会自动 delete/rebuild/reimage；
- 真实验收必须覆盖 create 失败、补偿 delete、重新 create、预约 active、释放和
  Pool 归零，并证明同一时刻不突破 Host、Image 和 Pool 上限。

## 与既有决策的关系

本 ADR 只补充 ADR-0006 中“任何中途失败必须补偿”的 provisioning 失败路径，不
改变 ADR-0029 对已进入服务的长期设备的保护，也不恢复 ADR-0028 已取消的健康故障
自动淘汰策略。
