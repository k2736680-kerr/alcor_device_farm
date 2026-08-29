# ADR-0028：受管虚拟设备以自动淘汰替换维持可用容量

## 状态

已完成。DF-053 本地自动化、真实 PostgreSQL 和测试 Server/Mac Host 的 CoreSimulator 删除补建验收均已通过。

## 背景

iOS Pool 已使用 `total_target/min_ready/max_concurrency` 表达固定容量，但 DF-048 的扩容统计把所有非 `deleted` Device 都计入目标。一个 Simulator 进入 `quarantined` 后仍占据目标数量，Controller 不会补建；若它同时是 `base_device_id`，活体模板健康校验还会阻断整个 Pool。Host 的 Agent 心跳在线也不能证明每台 Simulator 仍存在或可预约。

受管 Simulator 是可以由 Xcode CoreSimulator 重新创建的基础设施资源。让这类资源长期停留在隔离区并依赖管理员手工删除，与固定容量和长期可用目标冲突。真机、删除失败和资源身份不确定等场景仍需要保留隔离状态，不能删除设备域状态机或审计记录。

## 决策

1. `quarantined` 继续作为设备域内部安全状态和人工处置边界，但不再是受管 iOS Simulator 的正常终态。
2. Pool 内无活动 Reservation、技术 Session 或在途 Host Command 的故障 Simulator，由 Controller 复用既有 `delete` Host Command、Host Agent 和 CoreSimulator Provider 自动删除；删除成功后禁用原 membership、保留历史 Device 和审计，并在下一轮按原 Pool 目标创建全新 Simulator。
3. 自动替换不降低 `total_target/min_ready/max_concurrency`，也不复制旧设备数据。删除失败时保留隔离并停止自动补建，避免未知 CoreSimulator 资源仍存在时无限超建；管理员看到明确失败原因后再处理。
4. Pool 容量只把健康的可服务 Device，以及正在创建/启动的 Device 计入目标；`deleted`、`quarantined`、停止或不健康 Device 不满足可用容量。存在尚未安全删除的故障资源时，先完成删除，再补建。
5. `base_device_id` 在首次由管理员选择健康 Device 后同时代表已验证的 Host、Runtime 和 Device Type 配置快照。基础 Device 后续被自动替换时允许继续读取其非敏感静态能力；每次创建仍必须重新校验 Host 在线、未排空、心跳、当前 Runtime/Device Type 目录和实时容量。
6. Agent 明确标记每次 Provider inventory 是否完整。只有完整 inventory 连续缺失超过宽限的已登记 Simulator 才进入故障过渡态；inventory 请求失败只使 Host 进入 maintenance，不把空清单当作设备被删除。
7. Console 面向日常使用只突出“可用、使用中、恢复中、故障”可用性，不要求用户理解全部内部 lifecycle/health 组合；详细状态和原因仍保留给审计与排障。

## 后果

- `目标 2` 不再被 `1 可用 + 1 隔离` 误判为已经满足；
- CoreSimulator、Appium Session 或 inventory 出现确定故障后，系统自动清理并补回目标容量；
- 删除失败不会通过盲目创建掩盖资源泄漏，Pool 会明确显示故障和容量缺口；
- 真机和不可安全删除的资源仍可使用隔离，Android 现有状态机与 DaFit/STF/Appium 复用边界不变。
