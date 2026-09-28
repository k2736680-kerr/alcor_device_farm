# ADR-0038：失败创建占位自动补偿清理

## 状态

已确定；真实 Linux KVM 发布验收前不得标记 DF-070 completed。

## 背景

按需 Pool 的 `total_target=0` 依靠 pending Reservation 临时提高目标。一次 Android
Emulator create 命令耗尽重试后，旧实现会先让 Device 进入
`quarantined/unhealthy`。当前按需 Android Emulator 不再保存跨任务数据：任务结束后
目标归零，故障实例也没有继续保留和人工恢复的业务价值。

当 Pool `max_concurrency=1` 时，占位会永久满足登记容量，但不能被 Scheduler 使用，
后续 Reservation 一直 pending，系统也不会再创建 Emulator。

## 决策

1. 把同时满足以下条件的记录认定为失败创建占位：create 命令为
   `failed/timed_out`、没有成功 create、序列号仍为 `pending-*`、没有 ADB/Appium
   Endpoint，并且从未被 Host 心跳观察到。
2. create 命令耗尽重试后，Warm Pool 直接复用现有 delete Host Command、Host
   Agent 和 Provider Delete 清理该占位，不再先把占位暴露为故障隔离设备；Server
   不直接访问 Docker。
3. delete 完成前保留 Pool membership 且不创建替代实例；成功后 Device 只标记
   `deleted`、退出 Pool 并保留审计历史，下一轮按原有容量和退避规则补建。
4. 已成功创建、后来因 STF、Agent、Appium 或 OOM 进入 `quarantined/unhealthy`
   的 Android Emulator，在没有活动 Reservation、Session 或 Host Command 时同样
   自动删除；池仍有目标或等待任务时，删除完成后创建全新实例。
5. 活动任务占用的设备必须先走现有释放流程，不能由故障清理抢占删除。只有删除
   命令本身耗尽重试时才继续保留隔离并告警，因为此时无法确认宿主机上的
   Provider 资源已经释放；它继续占用登记容量，不通过盲目创建第三台设备掩盖资源
   泄漏。

## 后果

- 创建命令永久失败不会再出现在故障隔离目录，也不会锁死 `target=0` 的按需 Pool；
- 按需 Android Emulator 不保留故障隔离数据，健康故障会自动 delete 并按需重建；
- 真实验收必须覆盖 create 失败、补偿 delete、重新 create、预约 active、释放和
  Pool 归零，并证明同一时刻不突破 Host、Image 和 Pool 上限。

## 与既有决策的关系

本 ADR 补充 ADR-0006 中“任何中途失败必须补偿”的 provisioning 失败路径，并按
当前无状态按需设备要求取代 ADR-0029 对 Android Emulator 故障实例的长期保留策略。
iOS Simulator 和物理设备不在本次变更范围内。
