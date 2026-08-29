# ADR-0029：长期设备优先采用非破坏恢复

## 状态

已完成。DF-056 的自动化、真实 PostgreSQL、生产 Server/Host 和三台真实设备验收均已通过。

## 背景

DF-038 已把 Android Emulator 和 iOS Simulator 定义为长期设备：预约释放只释放占用，不清空已安装 App、账号、缓存和文件。ADR-0028/DF-053 后续又把故障 iOS Simulator 视为一次性资源，允许自动删除后补建；Android Warm Pool 也会在隔离设备不再出现在最新心跳时把它排除在登记容量之外并创建替代设备。

真实运行证明，STF 短暂不可见、Agent 一次健康抖动、Appium 共享服务稳定过程和 inventory 短暂漂移并不等于 Provider 资源已损坏。自动删除补建会改变 Device 身份并永久丢失虚拟机内的数据，与长期设备目标冲突。

## 决策

1. 已登记且未显式删除的 Emulator/Simulator 始终占用 Pool 的登记容量。`quarantined` 只表示暂不调度，不产生自动补建缺口。
2. 取消 iOS 故障 Simulator 自动 delete/recreate。自动缩容只在管理员明确降低 Pool 目标时运行；人工 delete、rebuild、reimage 继续要求权限、原因和二次确认。
3. 系统自动产生的 Android STF/Agent 健康隔离允许原 Device 自愈：先重复探测；探测恢复时原 ID 直接回到原预约状态或 `ready/healthy`；持续失败且设备空闲时最多排队一次 `restart` Host Command。Restart 只重启现有 Provider 资源，保留数据卷、Device ID 和 Pool membership。
4. 自动 restart 失败后保留 `quarantined/unhealthy` 并告警，不自动 rebuild、reimage、delete 或创建替代设备。管理员可对隔离设备再次执行非破坏 restart；破坏性操作只能人工明确选择。
5. 人工隔离不会被心跳或健康协调器自动解除。只有具有系统故障原因的隔离状态可自动恢复。
6. 正式启用前允许在可恢复备份和维护窗口内物理清理测试产生的 Reservation、Session、健康事件、审计、命令、任务和 deleted Device；必须保留当前 Host、Pool、Image、三台 Device 及对应 Provider 资源。

## 后果

- 短暂 STF、Agent、Appium 或 inventory 抖动不会清空设备数据；
- 故障设备会造成可用容量缺口并清晰告警，但不会被新设备掩盖；
- 自动恢复限定为可证明非破坏的原机探测和一次重启；
- 需要恢复干净环境时仍可由管理员明确执行 rebuild/reimage/delete。

## 取代关系

本 ADR 取代 ADR-0028 中“故障 iOS Simulator 自动删除补建”和“隔离设备不占登记容量”的决定；ADR-0028 的完整 inventory、简化可用性展示、容量缺口指标和删除失败防超建部分继续有效。
